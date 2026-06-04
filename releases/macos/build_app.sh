#!/usr/bin/env bash
#
# Build and stage Misty.app for macOS from the prepared release payload.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

DEFAULT_MISTY_BINARY="$ROOT/build/release/misty"
if [[ ! -e "$DEFAULT_MISTY_BINARY" && -e "$ROOT/build/release/bin/misty" ]]; then
    DEFAULT_MISTY_BINARY="$ROOT/build/release/bin/misty"
fi

MISTY_BINARY="${MISTY_MACOS_BINARY:-$DEFAULT_MISTY_BINARY}"
ASSETS_SOURCE_DIR="${MISTY_MACOS_ASSETS_DIR:-$ROOT/releases/misty/assets}"
MACOS_DIR="$APP/Contents/MacOS"
RES_DIR="$APP/Contents/Resources"
ASSETS_DIR="$MACOS_DIR/assets"
FRAMEWORKS_DIR="$APP/Contents/Frameworks"
INFO_PLIST="$APP/Contents/Info.plist"
APP_ICON="$RES_DIR/AppIcon.icns"
ICON_SOURCE="${MISTY_MACOS_ICON_SOURCE:-$ROOT/assets/logos/misty.icns}"
BUNDLE_ID="${MISTY_MACOS_BUNDLE_ID:-com.mistysys.Misty}"

project_version() {
    local version=""
    version="$(sed -n 's/^project([^)]*VERSION \([^ )]*\)).*/\1/p' "$ROOT/CMakeLists.txt" | head -n 1)"
    printf '%s' "${version:-1.0}"
}

generate_info_plist() {
    local version
    version="$(project_version)"

    cat > "$INFO_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>Misty</string>
    <key>CFBundleExecutable</key>
    <string>misty</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>$BUNDLE_ID</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>Misty</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$version</string>
    <key>CFBundleVersion</key>
    <string>$version</string>
    <key>LSMinimumSystemVersion</key>
    <string>12.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF
}

generate_app_icon() {
    local clean_iconset

    if [[ "$ICON_SOURCE" == *.icns ]]; then
        [[ -f "$ICON_SOURCE" ]] || {
            echo "error: icon source missing: $ICON_SOURCE" >&2
            exit 1
        }
        cp "$ICON_SOURCE" "$APP_ICON"
        return
    fi

    require_command iconutil
    require_command sips

    if [[ ! -f "$ICON_SOURCE" ]]; then
        echo "error: icon source missing: $ICON_SOURCE" >&2
        echo "Provide an .icns file or a 1024x1024 PNG via MISTY_MACOS_ICON_SOURCE." >&2
        exit 1
    fi

    clean_iconset="$(mktemp -d "${TMPDIR:-/tmp}/misty-icon.XXXXXX")"
    mv "$clean_iconset" "$clean_iconset.iconset"
    clean_iconset="$clean_iconset.iconset"
    trap 'rm -rf "$clean_iconset"' RETURN

    cp "$ICON_SOURCE" "$clean_iconset/icon_512x512@2x.png"
    sips -z 16 16 "$ICON_SOURCE" --out "$clean_iconset/icon_16x16.png" >/dev/null
    sips -z 32 32 "$ICON_SOURCE" --out "$clean_iconset/icon_16x16@2x.png" >/dev/null
    sips -z 32 32 "$ICON_SOURCE" --out "$clean_iconset/icon_32x32.png" >/dev/null
    sips -z 64 64 "$ICON_SOURCE" --out "$clean_iconset/icon_32x32@2x.png" >/dev/null
    sips -z 128 128 "$ICON_SOURCE" --out "$clean_iconset/icon_128x128.png" >/dev/null
    sips -z 256 256 "$ICON_SOURCE" --out "$clean_iconset/icon_128x128@2x.png" >/dev/null
    sips -z 256 256 "$ICON_SOURCE" --out "$clean_iconset/icon_256x256.png" >/dev/null
    sips -z 512 512 "$ICON_SOURCE" --out "$clean_iconset/icon_256x256@2x.png" >/dev/null
    sips -z 512 512 "$ICON_SOURCE" --out "$clean_iconset/icon_512x512.png" >/dev/null

    iconutil -c icns "$clean_iconset" -o "$APP_ICON"
    rm -rf "$clean_iconset"
    trap - RETURN
}

[[ -x "$MISTY_BINARY" ]] || {
    echo "error: Misty binary missing or not executable: $MISTY_BINARY" >&2
    echo "Build release first or set MISTY_MACOS_BINARY=/path/to/misty." >&2
    exit 1
}
[[ -d "$ASSETS_SOURCE_DIR" ]] || {
    echo "error: assets directory missing: $ASSETS_SOURCE_DIR" >&2
    echo "Set MISTY_MACOS_ASSETS_DIR=/path/to/assets if needed." >&2
    exit 1
}

step "Creating bundle layout"
rm -rf "$APP"
mkdir -p "$MACOS_DIR" "$RES_DIR" "$ASSETS_DIR" "$FRAMEWORKS_DIR"

step "Generating Info.plist"
generate_info_plist

step "Generating AppIcon.icns"
generate_app_icon

step "Staging release payload"
install -m 0755 "$MISTY_BINARY" "$MACOS_DIR/misty"
if command -v xattr >/dev/null 2>&1; then
    xattr -cr "$APP"
fi

require_command dylibbundler

step "Bundling non-system dylibs"
dylibbundler -od -of -cd -b \
    -x "$MACOS_DIR/misty" \
    -d "$FRAMEWORKS_DIR" \
    -p "@executable_path/../Frameworks/"

if otool -L "$MACOS_DIR/misty" | grep -q "/opt/homebrew"; then
    echo "error: misty still references /opt/homebrew after bundling:" >&2
    otool -L "$MACOS_DIR/misty" | grep "/opt/homebrew" >&2
    exit 1
fi

step "Staging runtime assets"
rsync -a --delete \
    --exclude '.DS_Store' \
    --exclude '*.iconset' \
    "$ASSETS_SOURCE_DIR/" "$ASSETS_DIR/"

step "Verifying bundle layout"
for f in \
    "$MACOS_DIR/misty" \
    "$ASSETS_DIR/themes/default.css" \
    "$INFO_PLIST" \
    "$APP_ICON"
do
    [[ -e "$f" ]] || { echo "error: missing after stage: $f" >&2; exit 1; }
done

for bin in misty; do
    if ! file "$MACOS_DIR/$bin" | grep -q Mach-O; then
        echo "error: $bin is not a Mach-O binary" >&2
        exit 1
    fi
done

echo ""
echo "OK: staged Misty.app at $APP"
du -sh "$APP"
