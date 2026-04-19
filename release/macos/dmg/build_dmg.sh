#!/usr/bin/env bash
#
# Build and sign the Misty macOS DMG.
#
# Input:  release/macos/Misty.app (must already be built + codesigned by
#         scripts/macos/build_macos_app.sh + scripts/macos/sign_macos_app.sh).
# Output: release/macos/Misty-<version>.dmg (signed, NOT yet notarized).
#
# Flow:
#   1. create-dmg (homebrew) -> unsigned .dmg with drag-to-Applications layout
#   2. codesign              -> signs the .dmg with Developer ID Application
#
# Signing a DMG is not required for Gatekeeper (the .app inside is what gets
# validated), but Apple's notary service rejects unsigned DMGs, so we sign
# here. After this, run submit_dmg.sh to notarize + staple.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
APP="$ROOT/release/macos/Misty.app"
DMG_DIR="$ROOT/release/macos/dmg"
BUILD_DIR="$DMG_DIR/build"

ENV_FILE="$ROOT/scripts/.signing.env"
[[ -f "$ENV_FILE" ]] || { echo "error: $ENV_FILE missing (copy .signing.env.example)" >&2; exit 1; }
# shellcheck disable=SC1090
source "$ENV_FILE"

: "${DEVELOPER_ID:?DEVELOPER_ID must be set in scripts/.signing.env}"

step() { printf "\n==> %s\n" "$1"; }

# --- 1. Preflight ---------------------------------------------------------
[[ -d "$APP" ]] || { echo "error: $APP missing. Run build_macos_app.sh." >&2; exit 1; }

if ! command -v create-dmg >/dev/null 2>&1; then
    echo "error: create-dmg not found on PATH." >&2
    echo "Install with: brew install create-dmg" >&2
    exit 1
fi

# Require the .app to be signed before we wrap it. An unsigned .app inside a
# signed DMG still launches with the DMG quarantine bit, but Gatekeeper will
# fail on first run — cheapest to catch here.
if ! codesign --verify --deep --strict "$APP" >/dev/null 2>&1; then
    echo "error: $APP is not signed. Run scripts/macos/sign_macos_app.sh first." >&2
    exit 1
fi

VERSION="$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP/Contents/Info.plist")"
[[ -n "$VERSION" ]] || { echo "error: could not read CFBundleShortVersionString" >&2; exit 1; }
DMG_OUT="$ROOT/release/macos/Misty-${VERSION}.dmg"
UNSIGNED_DMG="$BUILD_DIR/Misty-${VERSION}-unsigned.dmg"
STAGE_DIR="$BUILD_DIR/stage"

# --- 2. Stage payload -----------------------------------------------------
# create-dmg copies the entire source folder's contents into the image, so
# stage Misty.app alone to keep the DMG clean (no stray files, no .DS_Store
# from the parent dir).
step "Staging payload"
rm -rf "$BUILD_DIR"
mkdir -p "$STAGE_DIR"
# ditto preserves extended attributes, symlinks, and resource forks — cp -R
# can corrupt .app bundles on APFS in edge cases.
ditto "$APP" "$STAGE_DIR/Misty.app"

# --- 3. Build the DMG -----------------------------------------------------
# --no-internet-enable: macOS 10.15+ deprecated the "internet-enabled" flag.
# create-dmg leaves a preexisting output file in place, so remove first.
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

# --- 4. Sign the DMG ------------------------------------------------------
# Developer ID Application — same cert that signed the .app. DMGs don't take
# entitlements or hardened runtime; --timestamp is the only flag notary cares
# about here.
step "Signing DMG with $DEVELOPER_ID"
codesign --force --timestamp --sign "$DEVELOPER_ID" "$UNSIGNED_DMG"

# Move the signed artifact to the release dir with the versioned name.
mv "$UNSIGNED_DMG" "$DMG_OUT"

# --- 5. Verify ------------------------------------------------------------
step "Verifying signature"
codesign --verify --verbose=2 "$DMG_OUT"

echo ""
echo "OK: signed DMG at $DMG_OUT"
ls -lh "$DMG_OUT"
echo ""
echo "Preview locally:   open \"$DMG_OUT\""
echo "Ship when happy:   $DMG_DIR/submit_dmg.sh"
