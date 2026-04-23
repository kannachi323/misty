#!/usr/bin/env bash
#
# End-to-end macOS DMG publication pipeline.

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

skip_test=0
skip_notarize=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-test)
            skip_test=1
            ;;
        --skip-notarize)
            skip_notarize=1
            ;;
        *)
            echo "error: unknown argument: $1" >&2
            echo "usage: $0 [--skip-test] [--skip-notarize]" >&2
            exit 1
            ;;
    esac
    shift
done

load_signing_env
prepare_signing_keychain_if_needed

"$PIPELINE_DIR/build_app.sh"
"$PIPELINE_DIR/sign_app.sh"

if [[ "$skip_test" -eq 0 ]]; then
    "$PIPELINE_DIR/test_app.sh"
else
    step "Skipping GUI smoke test"
    codesign --verify --deep --strict --verbose=2 "$APP"
fi

"$PIPELINE_DIR/build_dmg.sh"

if [[ "$skip_notarize" -eq 0 ]]; then
    "$PIPELINE_DIR/submit_dmg.sh"
fi

echo ""
echo "OK: macOS DMG pipeline finished"
echo "Artifact: $(dmg_path)"
