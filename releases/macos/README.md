# macOS Release Pipeline

This directory is the home for Misty's macOS release automation.

The app icon source of truth is `assets/logos/misty.icns`. Set
`MISTY_MACOS_ICON_SOURCE` to an `.icns` file or a 1024x1024 PNG to override it.

## Local flow

To make a local `.app` from the prepared release payload:

```sh
./releases/macos/build_app.sh
```

By default, the app binary is staged from `build/release/misty`, falling back
to `build/release/bin/misty` for the current CMake output layout. Assets are
copied from `releases/misty/assets` into `Contents/MacOS/assets`, next to the
binary, so the app can launch without a wrapper executable.

Useful overrides:

- `MISTY_MACOS_BINARY=/path/to/misty`
- `MISTY_MACOS_ASSETS_DIR=/path/to/assets`

For a signed and notarized DMG:

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
