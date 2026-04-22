# macOS Release Pipeline

This directory is the home for Misty's macOS release automation.

## Local flow

1. Create `releases/macos/.env`.
2. Fill in your Developer ID and notarization credentials.
3. Run `./releases/macos/publish_dmg.sh`.

Useful flags:

- `./releases/macos/publish_dmg.sh --skip-test`
- `./releases/macos/publish_dmg.sh --skip-notarize`

## CI secrets

The GitHub Actions workflow expects these secrets:

- `MACOS_DEVELOPER_ID`
- `MACOS_DEVELOPER_ID_CERT_P12`
- `MACOS_DEVELOPER_ID_CERT_PASSWORD`
- `MACOS_NOTARY_APPLE_ID`
- `MACOS_NOTARY_APP_PASSWORD`
- `MACOS_NOTARY_TEAM_ID`

The local `releases/macos/.env` file should define:

- `DEVELOPER_ID`
- `NOTARY_PROFILE`, or all of `APPLE_ID`, `APPLE_TEAM_ID`, and `APPLE_APP_PASSWORD`

## Outputs

- Bundled app: `releases/macos/Misty.app`
- Published DMG: `releases/Misty-<version>-<arch>.dmg`
