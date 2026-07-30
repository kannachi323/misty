# Misty Server architecture

Misty Server is organized around domain ownership with a small composition
root and explicit infrastructure boundaries.

## Repository map

```text
cmd/
  misty-server/             process entrypoint
  journal-collab-ticket/    collaboration ticket utility
  smart-library-eval/       Smart Library evaluation utility

internal/
  app/                      construction, routing, lifecycle, workers
  accounts/                 identity and account lifecycle contracts
  billing/                  subscriptions, credits, Stripe, entitlements
  spaces/                   collaboration and membership contracts
  journal/                  notes, drawings, tickets, control queues
  library/                  storage, uploads, organization, renditions
  discovery/                metadata, people, semantic and media search
  agents/                   model-independent agent runtime
  workflows/                workflow validation and execution
  integrations/             OAuth and provider contracts
  platform/
    config/                 the process-environment boundary
    postgres/               PostgreSQL adapter and Goose migrations
    httpapi/                HTTP adapter preserving the public API contract
    email/                  transactional email adapter
    metrics/                Prometheus adapter
    security/               security primitives
    telemetry/              analytics adapter

test/
  unit/                     domain and policy tests
  contract/http/            exact HTTP behavior and route inventory
  contract/postgres/        PostgreSQL behavior and migration contracts
  contract/architecture/    dependency and repository-layout rules
  integration/              cross-domain runtime flows
  testkit/                  shared database and repository test support
```

## Dependency rules

- `cmd` loads the executable and delegates to `internal/app`.
- `internal/app` is the only composition root.
- Domain packages define the models and consumer-owned ports they need.
- Concrete HTTP, PostgreSQL, email, metrics, and telemetry behavior stays
  behind `internal/platform`.
- Domain code must not import the composition root, HTTP adapter, or PostgreSQL
  adapter. Cross-domain behavior should use a narrow interface owned by the
  caller.
- Process environment reads belong in `internal/platform/config` or a command
  entrypoint. Constructors should prefer typed values.
- Background work implements `app.Worker` and is started and stopped with the
  application context.

These rules are executable in `test/contract/architecture`, not just
documentation.

## Adding code

Start new business behavior in the owning `internal/<domain>` package. Put an
interface in that domain's `ports.go` when it needs storage, delivery, search,
or another domain. Implement the interface in a focused adapter, then connect
it in `internal/app`.

HTTP compatibility is a protected contract. Add or change handlers in the HTTP
adapter and review `test/contract/http/app/routes.golden`. PostgreSQL changes
require an ordered migration under `internal/platform/postgres/migrations` and
a contract test. Do not edit an applied migration.

Tests never live beside production code:

- pure policy and use-case behavior goes in `test/unit/<domain>`;
- request/response behavior goes in `test/contract/http`;
- SQL behavior goes in `test/contract/postgres`;
- multi-domain flows go in `test/integration`;
- shared destructive-database setup goes in `test/testkit`.

The file-size check rejects handwritten Go files over 500 lines and reports
files over 300 lines. Prefer focused files in the 150–300 line range.
