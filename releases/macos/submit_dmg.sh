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
elif [[ -n "${MACOS_NOTARY_APPLE_ID:-}" && -n "${MACOS_NOTARY_TEAM_ID:-}" && -n "${MACOS_NOTARY_APP_PASSWORD:-}" ]]; then
    notary_args+=(--apple-id "$MACOS_NOTARY_APPLE_ID" --team-id "$MACOS_NOTARY_TEAM_ID" --password "$MACOS_NOTARY_APP_PASSWORD")
else
    echo "error: notarization credentials missing." >&2
    echo "Set NOTARY_PROFILE or MACOS_NOTARY_APPLE_ID, MACOS_NOTARY_TEAM_ID, and MACOS_NOTARY_APP_PASSWORD." >&2
    exit 1
fi

submit_with_retry() {
    local max_attempts="${MISTY_NOTARY_RETRY_ATTEMPTS:-4}"
    local delay_seconds="${MISTY_NOTARY_RETRY_DELAY_SECONDS:-20}"
    local attempt=1
    local output

    while (( attempt <= max_attempts )); do
        if output="$(xcrun notarytool submit "$DMG" "${notary_args[@]}" --wait 2>&1)"; then
            printf '%s\n' "$output"
            return 0
        fi

        printf '%s\n' "$output" >&2
        if ! grep -Eq 'serviceUnavailable|503 Slow Down|SlowDown' <<<"$output"; then
            return 1
        fi
        if (( attempt == max_attempts )); then
            return 1
        fi

        warn "Apple notary service is rate-limiting requests; retrying in ${delay_seconds}s (attempt ${attempt}/${max_attempts})"
        sleep "$delay_seconds"
        attempt=$((attempt + 1))
        delay_seconds=$((delay_seconds * 2))
    done
}

echo "Submitting: $DMG"

step "Submitting to notary service (this takes 1-15 min)"
submit_with_retry

step "Stapling notarization ticket"
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"

step "Gatekeeper assessment"
spctl --assess --type open --context context:primary-signature --verbose "$DMG"

echo ""
echo "OK: notarized + stapled DMG at $DMG"
ls -lh "$DMG"
