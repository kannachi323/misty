#!/usr/bin/env bash
#
# Notarize and staple the Misty macOS DMG.
#
# Input:  release/macos/Misty-<version>.dmg produced by build_dmg.sh
#         (must already be signed with Developer ID Application).
# Output: same DMG, with the notarization ticket stapled in-place.
#
# Flow:
#   1. notarytool submit --wait -> upload to Apple, block until ticket issues
#   2. stapler staple           -> embed ticket so Gatekeeper works offline
#   3. spctl --assess           -> sanity-check the final artifact
#
# Kept separate from build_dmg.sh so you can open the signed DMG locally,
# verify the drag-to-Applications layout looks right, and only then burn the
# 1-15 minute round-trip to Apple.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
APP="$ROOT/release/macos/Misty.app"

ENV_FILE="$ROOT/scripts/.signing.env"
[[ -f "$ENV_FILE" ]] || { echo "error: $ENV_FILE missing" >&2; exit 1; }
# shellcheck disable=SC1090
source "$ENV_FILE"

: "${NOTARY_PROFILE:?NOTARY_PROFILE must be set in scripts/.signing.env}"

step() { printf "\n==> %s\n" "$1"; }

# --- 1. Locate the DMG ---------------------------------------------------
# Version lives in the .app's Info.plist — single source of truth so
# build_dmg.sh and submit_dmg.sh always resolve the same filename.
[[ -d "$APP" ]] || { echo "error: $APP missing. Rebuild first." >&2; exit 1; }
VERSION="$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP/Contents/Info.plist")"
DMG="$ROOT/release/macos/Misty-${VERSION}.dmg"

[[ -f "$DMG" ]] || {
    echo "error: $DMG not found. Run build_dmg.sh first." >&2
    exit 1
}

# Refuse to submit if the DMG isn't signed — notary will reject it anyway.
if ! codesign --verify --verbose=1 "$DMG" >/dev/null 2>&1; then
    echo "error: $DMG is not signed. Run build_dmg.sh first." >&2
    exit 1
fi

echo "Submitting: $DMG"

# --- 2. Notarize ---------------------------------------------------------
step "Submitting to notary service (this takes 1-15 min)"
xcrun notarytool submit "$DMG" \
    --keychain-profile "$NOTARY_PROFILE" \
    --wait

# --- 3. Staple -----------------------------------------------------------
step "Stapling notarization ticket"
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"

# --- 4. Final assessment -------------------------------------------------
# DMGs use --type open (the disk image itself); the .app inside gets its own
# --type execute assessment when Gatekeeper quarantines it on first launch.
step "Gatekeeper assessment"
spctl --assess --type open --context context:primary-signature --verbose "$DMG"

echo ""
echo "OK: notarized + stapled DMG at $DMG"
ls -lh "$DMG"
