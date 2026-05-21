#!/usr/bin/env bash
#
# Build and stage Misty.app for macOS.
#
# Re-runs the app + proxy builds, then copies fresh binaries and runtime
# assets into releases/macos/Misty.app. Template files that live in the .app
# are preserved; this script only replaces build outputs and bundled assets.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

MACOS_DIR="$APP/Contents/MacOS"
RES_DIR="$APP/Contents/Resources"
ASSETS_DIR="$RES_DIR/assets"
FRAMEWORKS_DIR="$APP/Contents/Frameworks"
INFO_PLIST="$APP/Contents/Info.plist"
APP_ICON="$RES_DIR/AppIcon.icns"
ICON_SOURCE="${MISTY_MACOS_ICON_SOURCE:-$ROOT/releases/macos/AppIcon-1024.png}"
BUNDLE_ID="${MISTY_MACOS_BUNDLE_ID:-com.misty.app}"

project_version() {
    local version=""
    if [[ -f "$ROOT/build/CMakeCache.txt" ]]; then
        version="$(sed -n 's/^CMAKE_PROJECT_VERSION:STATIC=//p' "$ROOT/build/CMakeCache.txt" | head -n 1)"
    fi
    if [[ -z "$version" ]]; then
        version="$(sed -n 's/^project([^)]*VERSION \\([^ )]*\\)).*/\\1/p' "$ROOT/CMakeLists.txt" | head -n 1)"
    fi
    printf '%s' "${version:-1.0}"
}

ensure_bundle_layout() {
    local version
    version="$(project_version)"

    mkdir -p "$MACOS_DIR" "$RES_DIR" "$ASSETS_DIR" "$FRAMEWORKS_DIR"

    if [[ ! -f "$INFO_PLIST" ]]; then
        step "Generating Info.plist"
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
    <string>Misty</string>
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
    fi

    if [[ ! -f "$APP_ICON" ]]; then
        local clean_iconset
        step "Generating AppIcon.icns"
        require_command iconutil
        require_command sips

        if [[ ! -f "$ICON_SOURCE" ]]; then
            echo "error: icon source missing: $ICON_SOURCE" >&2
            echo "Provide a 1024x1024 PNG at that path or set MISTY_MACOS_ICON_SOURCE." >&2
            exit 1
        fi

        clean_iconset="$(mktemp -d "${TMPDIR:-/tmp}/misty-icon.XXXXXX.iconset")"
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
    fi

    if [[ ! -f "$ASSETS_DIR/misty.env" ]]; then
        step "Generating default bundled misty.env"
        cat > "$ASSETS_DIR/misty.env" <<EOF
PROXY_SERVICE_URL=http://127.0.0.1:3000
EOF
    fi
}

if [[ ! -f "$ROOT/build/CMakeCache.txt" ]]; then
    step "Configuring app build directory"
    cmake_args=(-S "$ROOT" -B "$ROOT/build" -DCMAKE_BUILD_TYPE=Release)
    if [[ -n "${MISTY_CMAKE_GENERATOR:-}" ]]; then
        cmake_args+=(-G "$MISTY_CMAKE_GENERATOR")
    fi
    if [[ -n "${MISTY_CMAKE_EXTRA_ARGS:-}" ]]; then
        # shellcheck disable=SC2206
        extra_args=(${MISTY_CMAKE_EXTRA_ARGS})
        cmake_args+=("${extra_args[@]}")
    fi
    cmake "${cmake_args[@]}"
fi

step "Building app (misty)"
cmake --build "$ROOT/build" --target misty --config Release

step "Building proxy (misty-proxy, release)"
make -C "$ROOT/proxy" release

step "Building launcher (Contents/MacOS/Misty)"
LAUNCHER_SRC="$RELEASE_DIR/launcher.c"
LAUNCHER_OUT="$RELEASE_DIR/launcher.bin"
[[ -f "$LAUNCHER_SRC" ]] || { echo "error: $LAUNCHER_SRC missing" >&2; exit 1; }
cc -O2 -Wall -Wextra -mmacosx-version-min=12.0 -o "$LAUNCHER_OUT" "$LAUNCHER_SRC"

ensure_bundle_layout

for required in \
    "$INFO_PLIST" \
    "$APP_ICON" \
    "$ASSETS_DIR/misty.env" \
    "$ROOT/misty.conf"
do
    [[ -e "$required" ]] || { echo "error: template file missing: $required" >&2; exit 1; }
done

step "Staging binaries"
install -m 0755 "$ROOT/build/bin/misty" "$MACOS_DIR/misty-bin"
install -m 0755 "$ROOT/proxy/dist/misty-proxy" "$MACOS_DIR/misty-proxy"
install -m 0755 "$LAUNCHER_OUT" "$MACOS_DIR/Misty"
rm -f "$MACOS_DIR/misty-pwd-helper" "$MACOS_DIR/restic"

require_command dylibbundler

step "Bundling Homebrew dylibs into Contents/Frameworks"
dylibbundler -od -of -cd -b \
    -x "$MACOS_DIR/misty-bin" \
    -d "$FRAMEWORKS_DIR" \
    -p "@executable_path/../Frameworks/"

if otool -L "$MACOS_DIR/misty-bin" | grep -q "/opt/homebrew"; then
    echo "error: misty-bin still references /opt/homebrew after bundling:" >&2
    otool -L "$MACOS_DIR/misty-bin" | grep "/opt/homebrew" >&2
    exit 1
fi

step "Staging runtime assets from assets"
mkdir -p "$ASSETS_DIR"
rsync -a --delete \
    --exclude 'misty.env' \
    --exclude 'misty.conf' \
    "$ROOT/assets/" "$ASSETS_DIR/"
install -m 0644 "$ROOT/misty.conf" "$RES_DIR/misty.conf"
rm -f "$ASSETS_DIR/misty.conf"

if [[ -d "$RES_DIR/proxy-icloud" ]]; then
    step "Removing orphaned proxy-icloud (migrated to rclone)"
    rm -rf "$RES_DIR/proxy-icloud"
fi

if [[ -d "$MACOS_DIR/assets" ]]; then
    step "Removing legacy Contents/MacOS/assets (moved to Resources/assets)"
    rm -rf "$MACOS_DIR/assets"
fi

step "Verifying bundle layout"
for f in \
    "$MACOS_DIR/Misty" \
    "$MACOS_DIR/misty-bin" \
    "$MACOS_DIR/misty-proxy" \
    "$ASSETS_DIR/misty.env" \
    "$RES_DIR/misty.conf" \
    "$APP/Contents/Info.plist" \
    "$RES_DIR/AppIcon.icns"
do
    [[ -e "$f" ]] || { echo "error: missing after stage: $f" >&2; exit 1; }
done

for bin in Misty misty-bin misty-proxy; do
    if ! file "$MACOS_DIR/$bin" | grep -q Mach-O; then
        echo "error: $bin is not a Mach-O binary" >&2
        exit 1
    fi
done

echo ""
echo "OK: staged Misty.app at $APP"
du -sh "$APP"
