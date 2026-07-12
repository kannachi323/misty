# Misty Server API

All routes are mounted at the root path and again under `/api`, except `/stripe/webhook`, which is root-only for Stripe.

JSON request bodies are limited to 8 KiB unless noted. Unknown fields and trailing JSON are rejected on handlers that use the shared decoder. Authenticated endpoints accept either the `misty_session` cookie or an `Authorization: Bearer <token>` header.

## Local Test Workflow

Run the full suite with:

```bash
./test.sh
```

When explicit `TEST_DB_*` variables are absent and the target DB host is local, the script starts `docker compose` Postgres, recreates `misty_server_test`, applies `db/migrations`, and then runs `go test ./... -count=1`.

Set `BOOTSTRAP_TEST_DB=0` to disable Docker/bootstrap behavior, or `BOOTSTRAP_TEST_DB=1` to force it.

To use an existing Postgres instance, set:

```bash
TEST_DB_HOST=localhost
TEST_DB_PORT=5432
TEST_DB_USER=misty
TEST_DB_PASSWORD=misty
TEST_DB_NAME=misty_server_test
TEST_DB_SSLMODE=disable
```

The test helpers refuse to reset a database whose name does not contain `test`.

## Account

`POST /register`

Body: `{ "name": string, "email": string, "password": string }`

Responses:

- `201` with `{ "user_id": string }`
- `400` when email/password are missing or JSON is invalid
- `409` when the email is already registered

The optional `X-Misty-Analytics-Enabled: true` header persists explicit analytics consent and permits the authoritative `user_registered` event. Missing/false consent remains disabled. `X-Misty-Platform` and `X-Misty-Release-Channel` are accepted only as allowlisted analytics properties.

`PUT /me/telemetry`

Authenticated JSON body with `analytics_enabled` and `error_reporting_enabled` booleans. This synchronizes the app's independent privacy preferences; verified billing transitions re-check the stored analytics preference before emitting PostHog events.

`POST /login`

Body: `{ "email": string, "password": string }`

Responses:

- `200` with `{ "user_id": string, "name": string, "email": string, "token": string }` and `misty_session`
- `400` when JSON is invalid or credentials are incomplete
- `401` when credentials are invalid

`POST /logout`

Deletes the current session when a token is present and clears the cookie.

## Password Reset

`POST /auth/forgot`

Body: `{ "email": string }`

Returns `202` for both known and unknown users to avoid account enumeration. Per email/IP key, this flow is rate-limited separately.

`GET /auth/reset/start?token=<token>`

Validates the emailed token, stores it in `misty_reset_token`, and redirects to `PASSWORD_RESET_URL`. Invalid or missing tokens redirect without setting a usable cookie.

`GET /auth/reset/validate`

Requires `misty_reset_token`. Returns `200` when valid and `404` when missing, invalid, or expired.

`POST /auth/reset`

Body: `{ "new_password": string }`

Requires `misty_reset_token`. On success, updates the password, deletes the token, clears the cookie, and returns `200`.

## Dashboard

All dashboard routes require authentication.

`GET /me`

Returns account fields plus public license state: `tier`, `status`, `allows_use`, `expires_at`, `trial_started_at`, and `license_device`. `billing` describes the public billing kind, cadence, subscription status, renewal date, scheduled cancellation, and portal availability. Stripe and internal license IDs are never exposed.

`PUT /me/profile`

Body: `{ "name": string }`

`PUT /me/device`

Body: `{ "device": string }`

`GET /me/settings`

Returns `{ "email_updates_enabled": bool }`.

`PUT /me/settings`

Body: `{ "email_updates_enabled": bool }`

## Billing

`POST /billing/trial/start`

Starts a one-time 14-day Pro trial only when the user has an active Basic license, has not started a trial before, and has no completed purchase history.

`POST /billing/checkout-session`

Body: `{ "tier": "pro" | "max", "interval": "month" | "year" }`

Returns a Stripe subscription Checkout `{ "url": string }`. Existing active subscribers must use the Customer Portal.

`POST /billing/credit-checkout-session`

Body: `{ "pack_id": "credits_1500" | "credits_3500" }`. Returns one-time Stripe Checkout `{ "url": string }`.
The stable pack identifiers grant 1,500,000 and 3,500,000 micro-credits respectively; their names are retained for API and Stripe metadata compatibility.

`POST /billing/portal-session`

Returns a Stripe Customer Portal `{ "url": string }` for an authenticated Stripe customer.

`GET /billing/usage`

Returns the current plan, monthly allowance and balance, purchased balance, reserved balance, next reset, total available credits, and consumption grouped by meter.

`POST /stripe/webhook`

Stripe-signed only. Handled event types:

- `checkout.session.completed`: records legacy lifetime purchases or grants a validated prepaid credit pack.
- `customer.subscription.created|updated|deleted`: persists canonical subscription state and recomputes the effective tier, preserving lifetime fallback rights.
- `charge.refunded`: marks the purchase refunded and downgrades the license to active basic.
- `charge.dispute.created`: marks the purchase disputed and downgrades the license to active basic.

Replay behavior: repeated checkout completion is idempotent. A checkout completion replay after a refund or dispute is ignored so it cannot reactivate a paid tier.

`POST /ai/complete`

Authenticated managed completion used by AI automation nodes. It consumes the shared `automation_ai` credit meter and returns `text`, the public Mika `model`, `credits_used`, and `credits_remaining`. Managed AI endpoints return structured HTTP `402` with `code: "credits_exhausted"` when a reservation cannot be funded.

## Waitlist

`POST /waitlist`

Body: `{ "name": string, "email": string }`

Returns `202` after storing the signup and sending the confirmation email. Duplicate emails remain accepted and do not send duplicate internal notifications.

## AI

All AI routes require authentication. AI JSON bodies are limited to 2 MiB.
Provider-producing routes additionally enforce a 32 KiB prompt limit, bounded
tool results, one in-flight request per user, 12 provider calls per minute, 120
per hour, and at most three provider calls per user turn. Rate limits return
structured HTTP `429` with `code: "rate_limited"` and `Retry-After`; requests are
never queued or retried automatically. Cancellation returns
`code: "request_canceled"` to the interrupted request.

`GET /ai/status`

Returns provider-neutral Mika status and a static running state. `provider` is
always `misty`; `model` is the subscription-selected `mika-low`, `mika-med`, or
`mika-high`; and `model_name` is the corresponding display name. Concrete AI
provider and model identifiers are never exposed by public AI endpoints.

`POST /ai/sessions`

Creates an in-memory agent session and returns `{ "session_id": string }`.

`POST /ai/sessions/{sessionID}/messages`

Body: `{ "mode": "ask" | "auto" | "full", "user_message": string, "active_root": string, "selected_paths": string[], "capabilities": { "tools": [{ "name": string, "risk": string }] } }`

`GET /ai/sessions/{sessionID}/events?after=<sequence>`

Returns ordered session events after the provided sequence.

`POST /ai/sessions/{sessionID}/tool-results`

Body: `{ "results": [{ "request_id": string, "name": string, "ok": bool, "result": object, "error": string }] }`

`POST /ai/sessions/{sessionID}/cancel`

Cancels an active session.

## Rate Limits

Limits are per client IP, method, and normalized path. `/api/...` and root aliases share the same rate-limit key. Defaults are 120 GETs/minute and 30 write requests/minute, with stricter limits for auth, waitlist, password reset, billing, and Stripe webhook routes.

By default, the limiter uses the TCP remote address and ignores `X-Forwarded-For`/`X-Real-IP` so direct clients cannot spoof their rate-limit key. Set `TRUST_PROXY_HEADERS=true` only when the server is reachable exclusively through a trusted reverse proxy that overwrites those headers.

## Database Security

Postgres row-level security is enabled and forced on all application tables. Repository methods set transaction-local `app.*` context values before querying so user-scoped operations only see their own rows, while password reset and Stripe webhook work runs under explicit service context. Run the API with a non-superuser Postgres role without `BYPASSRLS`; Postgres superusers bypass RLS by design.
