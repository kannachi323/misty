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

Returns account fields plus public license state: `tier`, `status`, `allows_use`, `expires_at`, `trial_started_at`, `license_device`, and `pro_upgrade_discount_eligible`. It intentionally does not expose the internal license ID.

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

Starts a one-time 14-day personal trial only when the user has an active basic license, has not started a trial before, and has no completed purchase history.

`POST /billing/checkout-session`

Body: `{ "tier": "personal" | "pro" }`

Returns `{ "url": string }`. Pro checkout may include an upgrade coupon when the user has an active paid personal purchase.

`POST /stripe/webhook`

Stripe-signed only. Handled event types:

- `checkout.session.completed`: validates user/license/tier metadata, activates paid tier, and records purchase.
- `charge.refunded`: marks the purchase refunded and downgrades the license to active basic.
- `charge.dispute.created`: marks the purchase disputed and downgrades the license to active basic.

Replay behavior: repeated checkout completion is idempotent. A checkout completion replay after a refund or dispute is ignored so it cannot reactivate a paid tier.

## Waitlist

`POST /waitlist`

Body: `{ "name": string, "email": string }`

Returns `202` after storing the signup and sending the confirmation email. Duplicate emails remain accepted and do not send duplicate internal notifications.

## AI

All AI routes require authentication. AI JSON bodies are limited to 2 MiB.

`GET /ai/status`

Returns provider/model status and a static running state.

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
