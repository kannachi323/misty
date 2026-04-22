#!/usr/bin/env bash

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$COMMON_DIR/../.." && pwd)"
PIPELINE_DIR="$ROOT/releases/macos"
RELEASE_DIR="$ROOT/releases/macos"
APP="$RELEASE_DIR/Misty.app"
SIGNING_ENV="$PIPELINE_DIR/.env"
LEGACY_SIGNING_ENV="$ROOT/scripts/.signing.env"

step() { printf "\n==> %s\n" "$1"; }
warn() { printf "warn: %s\n" "$1" >&2; }

require_command() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "error: $1 not found on PATH" >&2
        exit 1
    }
}

load_signing_env() {
    local env_file="$SIGNING_ENV"
    if [[ -f "$env_file" ]]; then
        :
    elif [[ -f "$LEGACY_SIGNING_ENV" ]]; then
        env_file="$LEGACY_SIGNING_ENV"
        warn "using legacy signing config at $LEGACY_SIGNING_ENV; move it to $SIGNING_ENV when convenient"
    else
        echo "error: signing config missing." >&2
        echo "Create $SIGNING_ENV with the required signing and notarization variables." >&2
        exit 1
    fi

    # shellcheck disable=SC1090
    source "$env_file"
}

app_version() {
    /usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP/Contents/Info.plist"
}

app_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        aarch64) printf 'arm64' ;;
        amd64) printf 'x86_64' ;;
        *) printf '%s' "$arch" ;;
    esac
}

dmg_path() {
    printf '%s/releases/Misty-%s-%s.dmg' "$ROOT" "$(app_version)" "$(app_arch)"
}
