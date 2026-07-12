# Misty analytics and error tracking

Misty uses one PostHog project behind separate TypeScript, Rust, and Go abstractions. Product analytics and anonymous error reporting are independent, opt-in preferences; both default to disabled. Development and tests never send remote telemetry. Autocapture, pageviews, form/element tracking, session replay, heatmaps, console capture, and network-payload capture are disabled.

## Ownership and sources of truth

| Metric | Canonical source | Event / system |
| --- | --- | --- |
| Direct desktop download request | Download website, server, or CDN (not present here) | `download_requested` |
| Apple downloads and deletions | App Store Connect | External report |
| Google Play installs and uninstalls, including compatible Chromebooks | Google Play Console | External report |
| First successful application open | Tauri client | `app_first_opened` |
| Registration | `misty-server`, after `CreateUser` succeeds | `user_registered` |
| Onboarding completion | Mobile first-launch client, because no server or active desktop onboarding transition exists | `onboarding_completed` |
| Stripe lifetime purchase activation | Verified `misty-server` Stripe webhook and purchase row | `subscription_started` |
| Stripe refund/dispute reversal | Verified webhook and authoritative purchase row | `subscription_canceled` |
| Application use and retention | Tauri foreground lifecycle | `app_session_started` |
| React/TypeScript failures | Root boundary plus uncaught error/rejection listeners | PostHog `$exception`, `runtime_layer=react` |
| Rust failures and supported panics | `posthog-rs` reporter and panic hook | PostHog `$exception`, `runtime_layer=rust` |

The installed app never emits `download_requested` or `app_uninstalled`. A download request is not a completed installation, and `app_first_opened` is only an activation proxy. Reinstalling after local app state is removed can create a new random installation ID. Direct Windows/macOS/Linux uninstall counts are unavailable; inactivity must not be labeled as an uninstall.

The current Stripe implementation is a one-time lifetime purchase, not a recurring subscription. It therefore has no authoritative renewal or natural-expiration transition; `subscription_renewed` and `subscription_expired` remain defined but are not emitted. Stripe's optional PostHog data source can supplement analysis, but Misty's verified webhook/database transition remains authoritative.

There is no refresh-token endpoint in the inspected server: it uses opaque session tokens/cookies. `authenticated_session_started` and `X-Misty-Session-Id` are reserved but are not emitted merely for ordinary authenticated API calls. The client `app_session_started` remains the retention source.

## Privacy and identity

- `install_id` and `session_id` are random UUIDs stored in private local app storage. They are never derived from hardware, hostnames, usernames, paths, or advertising identifiers.
- After authentication, the client identifies only by opaque Misty user ID with allowlisted account creation/plan properties. Logout resets PostHog identity but retains the installation ID.
- TypeScript and Rust redactors remove sensitive keys and scrub paths, tokens, emails, URLs, request data, file/content data, and user-entered context before exception capture.
- The Go server emits only fixed allowlisted registration and billing properties. Delivery is buffered, bounded, non-fatal, and sanitized failures never include payloads or credentials.
- Authenticated consent is synchronized to `misty-server` and stored as booleans. Registration is captured only when its explicit request header is enabled; Stripe lifecycle events re-check the current persisted account preference before capture, so opting out stops later server events.
- Changing either consent default requires privacy-policy and store-disclosure review.

React/WebView plus Rust monitoring does not guarantee capture of every Swift, Kotlin, WebView-process, OS-level, segmentation-fault, or out-of-memory crash. Native crash SDK evaluation is outside this minimal phase.

## Configuration and releases

Runtime/public values (the same project should be used in app and server):

```text
POSTHOG_PROJECT_TOKEN=phc_...
POSTHOG_HOST=https://us.i.posthog.com
POSTHOG_PROJECT_ID=...
VITE_APP_ENVIRONMENT=production
VITE_RELEASE_CHANNEL=production
VITE_DISTRIBUTION_CHANNEL=direct
MISTY_RELEASE_CHANNEL=production
```

The Go server additionally uses `MISTY_ENVIRONMENT` and optional `MISTY_SERVER_VERSION`. Remote server capture requires a configured staging/production environment and an internal/alpha/beta/production release channel. Missing values fail closed.

`.env.analytics` is ignored and loaded for local Vite/Rust builds. Production deployment should inject the same public values explicitly. Never put a PostHog personal key in client runtime code.

In PostHog, enable **Error tracking** for the project, keep session replay/autocapture disabled, and use the same project token/host in all runtimes. The client captures exceptions manually so the two Misty consent settings remain independent; do not enable project defaults that record console or network payloads.

Private frontend source-map upload requires a build-only `POSTHOG_API_KEY` with `error tracking:write` and `organization:read`, plus `POSTHOG_PROJECT_ID`. Store it in CI/local release secrets. Release builds enable hidden source maps, inject/upload them with `@posthog/rollup-plugin`, and delete maps after upload. Use the real `MISTY_RELEASE_VERSION` or CI commit SHA. Verify the upload under PostHog **Error tracking → Symbol sets**, then trigger a controlled non-production exception and inspect the resolved TypeScript frame and redaction before claiming symbolication works. No source-map credential is compiled into Misty.

This repository has no checked-in CI workflow, so source-map upload is configured but has not been executed or symbolication-verified. Add the build-only secret to the actual release system before shipping.

The Tauri CSP permits the selected US ingestion host explicitly. `https:` remains allowed because Misty connects to runtime-configured account and storage-provider HTTPS endpoints; no wildcard, unsafe script, or eval permission is added. Change the explicit ingestion host when moving PostHog regions.

## Misty Product Health dashboard

Create these PostHog insights and exclude `development`, `test`, and `connectivity_test`:

- Acquisition: `download_requested` by platform where the future download service emits it; external store downloads; `app_first_opened` by platform; clearly labeled request-to-activation comparison.
- Funnel: `app_first_opened → user_registered → onboarding_completed → subscription_started`, noting anonymous/identified and cross-source coverage limitations.
- Registration/onboarding: unique registrations daily/weekly by platform/channel; unique completions; registration-to-onboarding conversion and median completion time where identities connect.
- Billing: starts by provider/plan, cancellations, and onboarding-to-start conversion. Active subscribers come from Stripe/Misty billing state, never starts-minus-cancellations arithmetic.
- Retention: overall D1/W1/W4 (`app_session_started` to itself); registered and onboarded daily/weekly retention; DAU/WAU/MAU. Break down by platform, app version, release/distribution channel, and plan where safe.
- Error health: `$exception` by `runtime_layer`, platform, app version, and release channel; new issues per release, frequent unhandled errors, affected users, and error-free sessions where supported.

Recommended alerts: material frontend/Rust error spikes, a new issue affecting multiple users, release regressions, major drops in sessions/onboarding, and verified webhook failure spikes. Do not alert on single development errors or expected validation/cancellation/offline outcomes.

## External work

- Add `download_requested` only in the real website/download service after it resolves an installer or store redirect. For CDN delivery, use CDN request logs; do not fabricate `download_completed`.
- Reconcile App Store Connect downloads/deletions and Play Console installs/uninstalls with PostHog activation/retention reporting.
- Update App Store/Play privacy disclosures before changing telemetry defaults or shipping the new opt-in collection.
- Exact direct-desktop uninstall measurement remains unavailable. Installer hooks, an optional survey, or inferred churn may be evaluated later but must be labeled imperfect/inferred.
