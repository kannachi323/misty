#!/usr/bin/env bash
#
# Build and sign the Misty macOS DMG.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

BUILD_DIR="$RELEASE_DIR/build/dmg"
STAGE_DIR="$BUILD_DIR/stage"

load_signing_env
prepare_signing_keychain_if_needed

: "${MACOS_DEVELOPER_ID:?MACOS_DEVELOPER_ID must be set in releases/macos/.env}"

[[ -d "$APP" ]] || { echo "error: $APP missing. Run build_app.sh first." >&2; exit 1; }
require_command create-dmg

if ! codesign --verify --deep --strict "$APP" >/dev/null 2>&1; then
    echo "error: $APP is not signed. Run sign_app.sh first." >&2
    exit 1
fi

VERSION="$(app_version)"
ARCH="$(app_arch)"
DMG_OUT="$(dmg_path)"
UNSIGNED_DMG="$BUILD_DIR/Misty-${VERSION}-${ARCH}-unsigned.dmg"

step "Staging payload"
rm -rf "$BUILD_DIR"
mkdir -p "$STAGE_DIR"
ditto "$APP" "$STAGE_DIR/Misty.app"

step "Building DMG with create-dmg"
rm -f "$UNSIGNED_DMG"
create-dmg \
    --volname "Misty ${VERSION}" \
    --window-pos 200 120 \
    --window-size 540 380 \
    --icon-size 100 \
    --icon "Misty.app" 130 190 \
    --app-drop-link 410 190 \
    --hide-extension "Misty.app" \
    --format UDZO \
    --no-internet-enable \
    "$UNSIGNED_DMG" \
    "$STAGE_DIR"

step "Signing DMG with $MACOS_DEVELOPER_ID"
codesign_with_identity "$UNSIGNED_DMG" --force --timestamp

mkdir -p "$(dirname "$DMG_OUT")"
mv "$UNSIGNED_DMG" "$DMG_OUT"

step "Verifying signature"
codesign --verify --verbose=2 "$DMG_OUT"

echo ""
echo "OK: signed DMG at $DMG_OUT"
ls -lh "$DMG_OUT"
echo ""
echo "Preview locally:   open \"$DMG_OUT\""
echo "Ship when happy:   $PIPELINE_DIR/submit_dmg.sh"
