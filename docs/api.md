# Misty Server API

The canonical Spaces → Agents → Workflows contracts, privacy model, workflow-version semantics, and endpoint index are documented in [agent-architecture.md](agent-architecture.md).

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

Body: `{ "name": string, "username": string, "email": string, "password": string }`

`username` is required, normalized to lowercase, and must contain 3–30 ASCII
letters, numbers, or underscores.

Responses:

- `201` with the authenticated user, including `user_id` and `username`
- `400` when username/email/password are missing, the username is invalid, or JSON is invalid
- `409` when the email is already registered or the username is taken

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

Retired. Returns HTTP `410` with `code: "trial_checkout_required"`; trials begin through card-required Stripe Checkout.

`POST /billing/checkout-session`

Body: `{ "tier": "pro", "interval": "month" | "year" }`

Returns a Stripe subscription Checkout `{ "url": string }`. Eligible accounts receive a one-time 14-day trial; a payment method is required and the subscription auto-renews. Existing active subscribers must use the Customer Portal.

`POST /billing/credit-checkout-session`

Retired. Returns HTTP `410` with `code: "retired_product"`. Misty has no hosted-AI add-ons or automatic overages.

`POST /billing/portal-session`

Returns a Stripe Customer Portal `{ "url": string }` for an authenticated Stripe customer.

`GET /billing/usage`

Returns `plan`, owner-pooled `storage` usage and limits, `hosted_ai.used_ratio` and Monday reset time, plus trial/subscription state when applicable. Internal amounts, provider rates, and ledger entries are not exposed.

`POST /stripe/webhook`

Stripe-signed only. Handled event types:

- `checkout.session.completed`: acknowledges Pro subscription Checkout; retired one-time products are ignored.
- `customer.subscription.created|updated|deleted`: persists canonical state and grants Pro while active or trialing, including through cancellation's effective end.

Replay behavior is idempotent. Subscription state is derived from the latest Stripe event and period end.

`POST /ai/complete`

Authenticated managed completion used by AI automation nodes. It consumes the requesting member's shared weekly hosted-AI pool and returns `text`, automatic-routing metadata, `hosted_ai_used_ratio`, and `hosted_ai_reset_at`. Managed AI endpoints return structured HTTP `402` with `code: "hosted_ai_limit_reached"` when a reservation cannot be funded.

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

Returns provider-neutral agent status and a static running state. `provider` is
always `misty`; `model` is `automatic`; and `model_name` is `Automatic`.

`GET /ai/models`

Returns the server-controlled, chat-capable gateway model catalog and its catalog version. The same catalog is available on Free and Pro. Provider prices and internal rate-card data are never returned.

`POST /ai/sessions`

Body may include `agent_id`, `space_id`, and `model_id`. Creates a resumable agent session and returns its session ID plus the bound Agent, Space, model, and catalog version. A pinned model that is no longer in the catalog returns `agent_model_unavailable`; Misty never silently substitutes it.

`POST /ai/sessions/{sessionID}/messages`

Body: `{ "mode": "ask" | "auto" | "full", "user_message": string, "active_root": string, "selected_paths": string[], "capabilities": { "tools": [{ "name": string, "risk": string }] } }`

`GET /ai/sessions/{sessionID}/events?after=<sequence>`

Returns ordered session events after the provided sequence.

`POST /ai/sessions/{sessionID}/tool-results`

Body: `{ "results": [{ "request_id": string, "name": string, "ok": bool, "result": object, "error": string }] }`

`POST /ai/sessions/{sessionID}/cancel`

Cancels an active session.

## Personal Agents

All Personal Agent routes require authentication. Agent configuration and grant management are owner-only.

- `GET|POST /agents`
- `GET|PATCH|DELETE /agents/{agentID}`
- `GET|PUT /agents/{agentID}/space-grants`
- `GET /spaces/{spaceID}/chat-agents` returns the requester's enabled Agents plus presentation-only records for Agents granted in that Space

Agents are private by default. A grant can allow everyone in a Space or selected current members. Instructions, tool/context permissions, direct conversations, and memory remain private to the owner; shared invocations use memory isolated by invoker, Agent, and Space. Effective tools are intersected with the invoking member's permissions, model capabilities, and runtime safety policy.

Space chat stores Agent mentions as structured spans containing `agent_id`. Access is checked when the message is accepted and again immediately before the run. Shared replies are ordinary Space messages visible to that conversation's readers, while Hosted AI usage is charged to the invoking member.

## Space Smart Library Search

- `GET /spaces/{spaceID}/library/search/semantic?q=...` searches one Space and falls back to lexical matching when semantic usage is unavailable.
- `GET /search/spaces?q=...&limit=...` creates at most one query embedding, searches only Spaces where the requester can view Library content, and returns Space/item identity plus a deep link.

Library intelligence jobs persist the billing user that initiated enablement, reindexing, or upload. Hosted AI exhaustion requeues optional analysis for the wallet reset and never blocks the underlying upload.

## Rate Limits

Limits are per client IP, method, and normalized path. `/api/...` and root aliases share the same rate-limit key. Defaults are 120 GETs/minute and 30 write requests/minute, with stricter limits for auth, waitlist, password reset, billing, and Stripe webhook routes.

By default, the limiter uses the TCP remote address and ignores `X-Forwarded-For`/`X-Real-IP` so direct clients cannot spoof their rate-limit key. Set `TRUST_PROXY_HEADERS=true` only when the server is reachable exclusively through a trusted reverse proxy that overwrites those headers.

## Database Security

Postgres row-level security is enabled and forced on all application tables. Repository methods set transaction-local `app.*` context values before querying so user-scoped operations only see their own rows, while password reset and Stripe webhook work runs under explicit service context. Run the API with a non-superuser Postgres role without `BYPASSRLS`; Postgres superusers bypass RLS by design.
