#!/usr/bin/env bash
#
# Notarize and staple the Misty macOS DMG.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

load_signing_env

[[ -d "$APP" ]] || { echo "error: $APP missing. Rebuild first." >&2; exit 1; }
DMG="$(dmg_path)"

[[ -f "$DMG" ]] || {
    echo "error: $DMG not found. Run build_dmg.sh first." >&2
    exit 1
}

if ! codesign --verify --verbose=1 "$DMG" >/dev/null 2>&1; then
    echo "error: $DMG is not signed. Run build_dmg.sh first." >&2
    exit 1
fi

notary_args=()
if [[ -n "${NOTARY_PROFILE:-}" ]]; then
    notary_args+=(--keychain-profile "$NOTARY_PROFILE")
elif [[ -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_PASSWORD:-}" ]]; then
    notary_args+=(--apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PASSWORD")
else
    echo "error: notarization credentials missing." >&2
    echo "Set NOTARY_PROFILE or APPLE_ID, APPLE_TEAM_ID, and APPLE_APP_PASSWORD." >&2
    exit 1
fi

echo "Submitting: $DMG"

step "Submitting to notary service (this takes 1-15 min)"
xcrun notarytool submit "$DMG" "${notary_args[@]}" --wait

step "Stapling notarization ticket"
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"

step "Gatekeeper assessment"
spctl --assess --type open --context context:primary-signature --verbose "$DMG"

echo ""
echo "OK: notarized + stapled DMG at $DMG"
ls -lh "$DMG"
