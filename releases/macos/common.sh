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

base64_decode_to_file() {
    local encoded="$1"
    local out_path="$2"

    if printf '' | base64 --decode >/dev/null 2>&1; then
        printf '%s' "$encoded" | base64 --decode > "$out_path"
    elif printf '' | base64 -D >/dev/null 2>&1; then
        printf '%s' "$encoded" | base64 -D > "$out_path"
    else
        echo "error: unable to find a working base64 decode flag" >&2
        exit 1
    fi
}

prepare_signing_keychain_if_needed() {
    local cert_path keychain_password temp_dir

    if [[ -n "${MACOS_KEYCHAIN_PATH:-}" && -f "${MACOS_KEYCHAIN_PATH}" ]]; then
        return 0
    fi

    if [[ -z "${MACOS_DEVELOPER_ID_CERT_P12:-}" ]]; then
        return 0
    fi

    : "${MACOS_DEVELOPER_ID_CERT_PASSWORD:?MACOS_DEVELOPER_ID_CERT_PASSWORD must be set when MACOS_DEVELOPER_ID_CERT_P12 is present}"

    require_command base64
    require_command security

    temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/misty-signing.XXXXXX")"
    cert_path="$temp_dir/misty-developer-id.p12"
    export MACOS_KEYCHAIN_PATH="$temp_dir/misty-signing.keychain-db"

    keychain_password="${MACOS_KEYCHAIN_PASSWORD:-$MACOS_DEVELOPER_ID_CERT_PASSWORD}"
    export MACOS_KEYCHAIN_PASSWORD="$keychain_password"

    base64_decode_to_file "$MACOS_DEVELOPER_ID_CERT_P12" "$cert_path"

    security create-keychain -p "$MACOS_KEYCHAIN_PASSWORD" "$MACOS_KEYCHAIN_PATH"
    security set-keychain-settings -lut 21600 "$MACOS_KEYCHAIN_PATH"
    security unlock-keychain -p "$MACOS_KEYCHAIN_PASSWORD" "$MACOS_KEYCHAIN_PATH"
    security import "$cert_path" \
        -P "$MACOS_DEVELOPER_ID_CERT_PASSWORD" \
        -A \
        -f pkcs12 \
        -k "$MACOS_KEYCHAIN_PATH"
    security set-key-partition-list -S apple-tool:,apple:,codesign: \
        -s -k "$MACOS_KEYCHAIN_PASSWORD" "$MACOS_KEYCHAIN_PATH"
    rm -f "$cert_path"

    if [[ -n "${MACOS_DEVELOPER_ID:-}" ]] && ! security find-identity -v -p codesigning "$MACOS_KEYCHAIN_PATH" | grep -qF "$MACOS_DEVELOPER_ID"; then
        echo "error: imported keychain does not contain signing identity:" >&2
        echo "  $MACOS_DEVELOPER_ID" >&2
        echo "Check that MACOS_DEVELOPER_ID_CERT_P12 is a base64-encoded Developer ID .p12 containing the private key." >&2
        exit 1
    fi
}

codesign_with_identity() {
    local target="$1"
    shift

    if [[ -n "${MACOS_KEYCHAIN_PATH:-}" ]]; then
        codesign "$@" --keychain "$MACOS_KEYCHAIN_PATH" --sign "$MACOS_DEVELOPER_ID" "$target"
    else
        codesign "$@" --sign "$MACOS_DEVELOPER_ID" "$target"
    fi
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
