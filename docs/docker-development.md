# Docker development environment

The root Compose application starts everything the Misty server needs:

- PostgreSQL with pgvector
- database migrations
- non-superuser application-role permissions after migrations
- a freshly built `misty-server` binary on port 8081
- Stripe CLI event forwarding to `/stripe/webhook`
- a temporary Cloudflare Tunnel back to the local API
- an isolated `misty-journal-collab-dev` Worker deployment configured to call
  the temporary tunnel

Each process runs in its own container. This keeps health checks, restarts, and
logs independent while still providing a single startup command.

## One-time setup

Copy `.env.example` to `.env` if the repository does not already have an
environment file. At minimum, configure the database values, Stripe test-mode
secret key, application settings required by the server, and:

```dotenv
CLOUDFLARE_API_TOKEN=...
```

The Cloudflare token needs permission to edit Workers in the account configured
by `cloudflare/journal-collab/wrangler.jsonc`. A host-side `wrangler login`
session cannot be used inside Docker.

Journal collaboration keys are generated automatically on the first startup
when both `.dev.vars` and `.secrets/server.env` are absent. A partial secret
setup is treated as an error to avoid accidentally rotating only half of a key
pair.

## Start

```sh
docker compose up --build
```

The API is available at <http://localhost:8081>. Compose waits for PostgreSQL,
migrations, and Stripe's signing secret before it starts the API. It then opens
the temporary tunnel and deploys the development Worker.

The Worker deployment is a one-shot container. An exited status of `0` for
`cloudflare-deploy` means the deployment completed successfully; the other
containers continue running.

Useful views:

```sh
docker compose ps
docker compose logs -f api stripe tunnel cloudflare-deploy
```

Stop the environment without deleting database data:

```sh
docker compose down
```

To also reset the local database and generated runtime handoff files:

```sh
docker compose down --volumes
```

The generated Journal collaboration keys are files in
`cloudflare/journal-collab` and are intentionally not removed by
`docker compose down --volumes`.
