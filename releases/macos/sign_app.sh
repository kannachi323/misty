#!/usr/bin/env bash
#
# Codesign releases/macos/Misty.app for Developer ID distribution.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

ENTITLEMENTS="$RELEASE_DIR/Misty.entitlements"

load_signing_env
prepare_signing_keychain_if_needed

if [[ -z "${MACOS_DEVELOPER_ID:-}" ]]; then
    echo "error: MACOS_DEVELOPER_ID not set in signing config" >&2
    exit 1
fi

[[ -d "$APP" ]] || { echo "error: $APP missing. Run build_app.sh first." >&2; exit 1; }
[[ -f "$ENTITLEMENTS" ]] || { echo "error: $ENTITLEMENTS missing." >&2; exit 1; }

identity_args=(-v -p codesigning)
if [[ -n "${MACOS_KEYCHAIN_PATH:-}" ]]; then
    identity_args+=("$MACOS_KEYCHAIN_PATH")
fi

if ! security find-identity "${identity_args[@]}" | grep -qF "$MACOS_DEVELOPER_ID"; then
    echo "error: signing identity not found in keychain:" >&2
    echo "  $MACOS_DEVELOPER_ID" >&2
    if [[ -n "${MACOS_KEYCHAIN_PATH:-}" ]]; then
        echo "Run: security find-identity -v -p codesigning \"$MACOS_KEYCHAIN_PATH\"" >&2
    else
        echo "Run: security find-identity -v -p codesigning" >&2
    fi
    exit 1
fi

step "Signing inner Mach-O binaries"
while IFS= read -r -d '' f; do
    if file "$f" | grep -q "Mach-O"; then
        echo "  sign: ${f#$APP/}"
        codesign_with_identity "$f" --force --timestamp --options runtime
    fi
done < <(find "$APP" -type f -print0)

MAIN_EXEC_NAME="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$APP/Contents/Info.plist")"
MAIN_EXEC="$APP/Contents/MacOS/$MAIN_EXEC_NAME"
[[ -e "$MAIN_EXEC" ]] || { echo "error: CFBundleExecutable missing: $MAIN_EXEC" >&2; exit 1; }

if ! file "$MAIN_EXEC" | grep -q "Mach-O"; then
    step "Signing main executable (script): $MAIN_EXEC_NAME"
    codesign_with_identity "$MAIN_EXEC" --force --timestamp
fi

step "Signing bundle"
codesign_with_identity "$APP" --force --timestamp --options runtime \
    --entitlements "$ENTITLEMENTS"

step "Verifying signature"
codesign --verify --deep --strict --verbose=2 "$APP"

step "Gatekeeper assessment (pre-notarization)"
if spctl --assess --type execute --verbose "$APP" 2>&1 | tee /tmp/misty-spctl.log; then
    echo "  (fully accepted — already notarized?)"
else
    if grep -q "Unnotarized Developer ID" /tmp/misty-spctl.log; then
        echo "  OK: signed with Developer ID, pending notarization (expected)."
    else
        echo "  error: Gatekeeper rejected for a reason other than notarization." >&2
        exit 1
    fi
fi
rm -f /tmp/misty-spctl.log

echo ""
echo "OK: signed $APP"
