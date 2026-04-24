#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RELEASE_DIR="$ROOT/release/linux"
APP_NAME="Misty"
ARCH="${ARCH:-x86_64}"
BUILD_DIR="$RELEASE_DIR/build/appimage"
APPDIR="$BUILD_DIR/${APP_NAME}.AppDir"
TOOLS_DIR="$BUILD_DIR/tools"
OUT_DIR="$RELEASE_DIR"
VERSION="${VERSION:-$(date +%Y.%m.%d)}"
ARTIFACT_NAME="misty.AppImage"
GO_CACHE_DIR="${GO_CACHE_DIR:-/tmp/misty-go-build-cache}"

step() { printf "\n==> %s\n" "$1"; }
warn() { printf "warning: %s\n" "$*" >&2; }
fail() { echo "error: $*" >&2; exit 1; }

download_tool() {
    local url="$1"
    local out="$2"
    if [[ -x "$out" ]]; then
        return 0
    fi
    mkdir -p "$(dirname "$out")"
    if command -v curl >/dev/null 2>&1; then
        curl -L --fail --output "$out" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -O "$out" "$url"
    else
        fail "curl or wget is required to fetch AppImage tooling"
    fi
    chmod +x "$out"
}

render_icon() {
    local src="$1"
    local out="$2"
    if command -v magick >/dev/null 2>&1; then
        magick "$src" -background none -gravity center -resize 512x512 -extent 512x512 "$out"
    elif command -v convert >/dev/null 2>&1; then
        convert "$src" -background none -gravity center -resize 512x512 -extent 512x512 "$out"
    else
        fail "ImageMagick is required to generate the AppImage icon"
    fi
}

step "Building client binaries"
cmake -S "$ROOT/client" -B "$ROOT/client/build"
cmake --build "$ROOT/client/build" --target misty_assets -j"$(nproc)"

step "Building proxy binaries"
mkdir -p "$ROOT/proxy/dist"
mkdir -p "$GO_CACHE_DIR"
(
    cd "$ROOT/proxy"
    GOCACHE="$GO_CACHE_DIR" go build -trimpath -ldflags="-s -w" -o dist/misty-proxy .
    GOCACHE="$GO_CACHE_DIR" go build -trimpath -ldflags="-s -w" -o dist/misty-pwd-helper ./cmd/misty-pwd-helper
)

step "Preparing AppDir"
rm -rf "$APPDIR"
mkdir -p \
    "$APPDIR/usr/bin" \
    "$APPDIR/usr/lib" \
    "$APPDIR/usr/share/applications" \
    "$APPDIR/usr/share/metainfo" \
    "$APPDIR/usr/share/icons/hicolor/512x512/apps" \
    "$APPDIR/usr/share/misty/assets" \
    "$APPDIR/usr/bin/assets"

install -m 0755 "$ROOT/client/build/bin/misty" "$APPDIR/usr/bin/misty"
install -m 0755 "$ROOT/client/build/bin/misty-plugin-sandbox" "$APPDIR/usr/bin/misty-plugin-sandbox"
install -m 0755 "$ROOT/proxy/dist/misty-proxy" "$APPDIR/usr/bin/misty-proxy"
install -m 0755 "$ROOT/proxy/dist/misty-pwd-helper" "$APPDIR/usr/bin/misty-pwd-helper"

rsync -a --delete --exclude '.DS_Store' \
    "$ROOT/client/build/bin/assets/" "$APPDIR/usr/share/misty/assets/"
install -m 0644 "$ROOT/client/misty.conf" "$APPDIR/usr/share/misty/misty.conf"
install -m 0644 "$RELEASE_DIR/misty.env" "$APPDIR/usr/share/misty/assets/misty.env"
install -m 0644 "$RELEASE_DIR/misty.env" "$APPDIR/usr/bin/assets/misty.env"

if [[ -d "$ROOT/client/build/bin/plugins" ]]; then
    rsync -a --delete "$ROOT/client/build/bin/plugins/" "$APPDIR/usr/bin/plugins/"
fi

install -m 0755 "$RELEASE_DIR/AppRun" "$APPDIR/AppRun"
install -m 0644 "$RELEASE_DIR/misty.desktop" "$APPDIR/misty.desktop"
install -m 0644 "$RELEASE_DIR/misty.desktop" "$APPDIR/usr/share/applications/misty.desktop"
install -m 0644 "$RELEASE_DIR/misty.appdata.xml" "$APPDIR/usr/share/metainfo/com.mistysys.Misty.appdata.xml"

step "Generating AppImage icon"
ICON_SRC="$ROOT/client/assets/logos/misty.png"
ICON_OUT="$APPDIR/misty.png"
render_icon "$ICON_SRC" "$ICON_OUT"
install -m 0644 "$ICON_OUT" "$APPDIR/usr/share/icons/hicolor/512x512/apps/misty.png"
cp "$ICON_OUT" "$APPDIR/.DirIcon"

if command -v desktop-file-validate >/dev/null 2>&1; then
    step "Validating desktop file"
    desktop-file-validate "$APPDIR/usr/share/applications/misty.desktop"
fi

if command -v appstreamcli >/dev/null 2>&1; then
    step "Validating AppStream metadata"
    appstreamcli validate --no-net "$APPDIR/usr/share/metainfo/com.mistysys.Misty.appdata.xml" || true
fi

step "Fetching AppImage tooling"
LINUXDEPLOY="$TOOLS_DIR/linuxdeploy-${ARCH}.AppImage"
APPIMAGETOOL="$TOOLS_DIR/appimagetool-${ARCH}.AppImage"
APPIMAGE_RUNTIME="$TOOLS_DIR/runtime-${ARCH}"
download_tool \
    "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-${ARCH}.AppImage" \
    "$LINUXDEPLOY"
download_tool \
    "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${ARCH}.AppImage" \
    "$APPIMAGETOOL"
download_tool \
    "https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-${ARCH}" \
    "$APPIMAGE_RUNTIME"

step "Bundling shared libraries"
set +e
APPIMAGE_EXTRACT_AND_RUN=1 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    -e "$APPDIR/usr/bin/misty" \
    -e "$APPDIR/usr/bin/misty-plugin-sandbox" \
    -d "$APPDIR/usr/share/applications/misty.desktop" \
    -i "$APPDIR/usr/share/icons/hicolor/512x512/apps/misty.png"
linuxdeploy_status=$?
set -e

if [[ $linuxdeploy_status -ne 0 ]]; then
    if find "$APPDIR/usr/lib" -mindepth 1 -print -quit | grep -q .; then
        warn "linuxdeploy exited with status $linuxdeploy_status after staging shared libraries; continuing"
    else
        fail "linuxdeploy failed before bundling shared libraries"
    fi
fi

step "Building AppImage"
mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/$ARTIFACT_NAME"
ARCH="$ARCH" APPIMAGE_EXTRACT_AND_RUN=1 "$APPIMAGETOOL" \
    --runtime-file "$APPIMAGE_RUNTIME" \
    "$APPDIR" \
    "$OUT_DIR/$ARTIFACT_NAME"

echo ""
echo "OK: built $OUT_DIR/$ARTIFACT_NAME"
