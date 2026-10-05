# Mechon

**Start your own bot hosting company.** Mechon is a self-hosted, multi-tenant panel for renting out Discord bot hosting on your own servers. Every bot runs in its own locked-down container, with limits the kernel enforces.

> Mechon is being rewritten from scratch. The plan is in [docs/spec-v0.md](docs/spec-v0.md) and the decisions behind it are in [docs/decisions.md](docs/decisions.md). Milestone 1 (panel skeleton with sign-in) is done; bots do not run yet.

## Stack

Go panel and agent, Postgres, a React web UI embedded in the panel binary, and Docker for isolation. One binary to install the panel, one per node.

## Development

You need Go 1.25+, Node 22+ with pnpm, and Postgres 15+.

```bash
createdb mechon_dev
make web          # build the UI once so the binary can embed it
make seed         # create the local dev admin (below)
make dev-panel    # API on :8080
make dev-web      # UI with hot reload on :5173
```

Open http://localhost:5173 and sign in with the dev admin:

| Email | Password |
|---|---|
| `admin@mechon.test` | `dev-password-123` |

These are for local development only. On a real install, create the first admin with `mechon init --email you@example.com` and choose a strong password.

| Variable | Meaning |
|---|---|
| `MECHON_DATABASE_URL` | Postgres connection string (required) |
| `MECHON_PUBLIC_URL` | URL browsers use to reach the panel (required; sets the allowed origin and secure cookies) |
| `MECHON_LISTEN` | Listen address, default `:8080` |
| `MECHON_TRUST_PROXY` | `true` to take the client IP from `X-Forwarded-For` behind a reverse proxy |

Tests: `createdb mechon_test && make test`. The Go integration tests run against a real Postgres and wipe that database each run.

## License

[MIT](LICENSE)
