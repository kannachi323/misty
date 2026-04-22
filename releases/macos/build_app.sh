#!/usr/bin/env bash
#
# Build and stage Misty.app for macOS.
#
# Re-runs the client + proxy builds, then copies fresh binaries and runtime
# assets into releases/macos/Misty.app. Template files that live in the .app
# are preserved — this script only replaces what comes from a build or from
# client/assets.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

MACOS_DIR="$APP/Contents/MacOS"
RES_DIR="$APP/Contents/Resources"
ASSETS_DIR="$RES_DIR/assets"

if [[ ! -f "$ROOT/client/build/CMakeCache.txt" ]]; then
    step "Configuring client build directory"
    cmake_args=(-S "$ROOT/client" -B "$ROOT/client/build" -DCMAKE_BUILD_TYPE=Release)
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

step "Building client (misty)"
cmake --build "$ROOT/client/build" --target misty --config Release

step "Building proxy (misty-proxy, release)"
make -C "$ROOT/proxy" release

step "Building launcher (Contents/MacOS/Misty)"
LAUNCHER_SRC="$RELEASE_DIR/launcher.c"
LAUNCHER_OUT="$RELEASE_DIR/launcher.bin"
[[ -f "$LAUNCHER_SRC" ]] || { echo "error: $LAUNCHER_SRC missing" >&2; exit 1; }
cc -O2 -Wall -Wextra -mmacosx-version-min=12.0 -o "$LAUNCHER_OUT" "$LAUNCHER_SRC"

if [[ ! -d "$APP" ]]; then
    echo "error: $APP does not exist." >&2
    echo "The .app template (Info.plist, launcher, AppIcon) must be in place." >&2
    exit 1
fi
for required in \
    "$APP/Contents/Info.plist" \
    "$RES_DIR/AppIcon.icns" \
    "$ASSETS_DIR/misty.env" \
    "$ROOT/client/misty.conf"
do
    [[ -e "$required" ]] || { echo "error: template file missing: $required" >&2; exit 1; }
done

step "Staging binaries"
install -m 0755 "$ROOT/client/build/bin/misty" "$MACOS_DIR/misty-bin"
install -m 0755 "$ROOT/proxy/dist/misty-proxy" "$MACOS_DIR/misty-proxy"
install -m 0755 "$LAUNCHER_OUT" "$MACOS_DIR/Misty"
rm -f "$MACOS_DIR/misty-pwd-helper" "$MACOS_DIR/restic"

require_command dylibbundler

FRAMEWORKS_DIR="$APP/Contents/Frameworks"
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

step "Staging runtime assets from client/assets"
mkdir -p "$ASSETS_DIR"
rsync -a --delete \
    --exclude 'misty.env' \
    --exclude 'misty.conf' \
    "$ROOT/client/assets/" "$ASSETS_DIR/"
install -m 0644 "$ROOT/client/misty.conf" "$RES_DIR/misty.conf"
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
