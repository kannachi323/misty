# macOS Release Pipeline

This directory is the home for Misty's macOS release automation.

The app icon source of truth is `releases/macos/AppIcon-1024.png`. The build
generates the macOS `.icns` bundle icon from that single 1024x1024 PNG.

## Local flow

1. Create `releases/macos/.env`.
2. Fill in your Developer ID and notarization credentials.
   You can also include `MACOS_DEVELOPER_ID_CERT_P12` and
   `MACOS_DEVELOPER_ID_CERT_PASSWORD` to mirror the GitHub Actions flow;
   the local scripts will import that base64 `.p12` into a temporary keychain
   automatically.
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

- `MACOS_DEVELOPER_ID`
- `MACOS_DEVELOPER_ID_CERT_P12` (optional, for CI-style local signing)
- `MACOS_DEVELOPER_ID_CERT_PASSWORD` (required when `...CERT_P12` is set)
- `MACOS_NOTARY_APPLE_ID`
- `MACOS_NOTARY_TEAM_ID`
- `MACOS_NOTARY_APP_PASSWORD`

## Outputs

- Bundled app: `releases/macos/Misty.app`
- Published DMG: `releases/Misty-<version>-<arch>.dmg`
