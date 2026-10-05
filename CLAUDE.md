# Mechon: project context

This is a greenfield rewrite. The old Rust code was deliberately deleted, so don't restore it or take it as a reference. Nothing from the old codebase constrains this one. Only the GitHub repo (280★, 40 forks) is being kept.

Full research and decision log: `/Users/macbook/GitHub/PlotTwist/04-mechon/README.md` and `/Users/macbook/GitHub/PlotTwist/04-mechon/research/market-2026.md`. Read them for the background.

## Working rules (founder)

- **Git:** commit as the founder (the configured git user, Jareer). Never add a `Co-Authored-By` Claude line or any AI attribution. **Never push**; the founder pushes. No history rewrites (rebase, reset, amend of pushed commits).
- **Don't over-engineer.** Use a boring, performant stack. The backend has to be solid. Isolation and multi-tenancy are the product, and everything else is secondary.
- Record important decisions in a `docs/decisions.md` in this repo, with date, decision and status.

## What we're building

A **self-hosted, multi-tenant bot hosting panel that hosting startups install on their servers to rent out bot hosting**. Think Pterodactyl, but purpose-built for bots. It starts with Discord bots and later covers other always-on workloads (Telegram/Slack bots, AI agents such as OpenClaw).

- **Buyer:** a hosting startup or operator (B2B). It installs Mechon, defines plans, onboards customers and sells hosting.
- **End user:** a bot developer. They deploy a bot, see logs and metrics, and start, stop or restart it.
- **Market:** about 12M active Discord bots and about 680k bot developers each month [unverified]. bot-hosting.net alone claims 500k+ devs. No serious open-source panel dedicated to bots exists (old Mechon was #1 at 280★). Pterodactyl (9.3k★) is built for game servers, and Coolify/Dokploy target HTTP apps with no quotas or resale. OpenClaw (391k★) created an agent-hosting boom, which is the second act.

## v0 requirements

1. **Real isolation.** No bot may touch another bot's files, memory, processes or network. (The old version ran bare child processes with a RAM-poll kill, which is not acceptable.)
2. **Proper multi-tenancy.** Operator → plans → customers → bots → deploys, with quotas enforced on the server.
3. **A web panel from day one,** not only a CLI.
4. **Templates:** discord.js, discord.py and Bun, later agents (OpenClaw). Deploy by folder upload, git, or API.
5. **Operator API + webhooks** so billing systems (Paymenter, WHMCS, Stripe) can plug in. **Don't build billing.**

## Final stack (decided 2026-10-04)

- **Go** for both the panel API (`net/http` or chi) and the **worker agent** (one per host). Both are single static binaries.
- **Postgres**, with **sqlc** for queries and **River** (a Postgres-backed job queue). **No Redis.**
- The **worker dials out to the panel over WebSocket** for commands, log streaming and stats. No pub/sub broker.
- **React + Vite + TanStack Query + shadcn/ui**, built to static files and **embedded in the Go binary** (`embed`), so operators install a single binary.
  - **Not Next.js.** A panel behind a login needs no SSR or SEO, Next.js adds a Node runtime, and it breaks the single-binary install. Next.js or Astro is only an option for a separate marketing and docs site.
- **Auth:** sessions in Postgres, argon2id passwords, API keys for CI and operators.
- **Isolation, v0:** the **Docker Engine API** (the Pterodactyl Wings model). Each bot gets its own container:
  - a non-root user and a read-only rootfs,
  - cgroup limits on memory, CPU and pids,
  - `cap-drop ALL` and `no-new-privileges`, with the default seccomp profile,
  - outbound-only networking,
  - its own volume with a disk quota.
- **Hardened tier:** gVisor via `--runtime=runsc`, which is one flag.
- Hide the container layer behind a Go **`Runtime` interface**, so calling crun directly stays possible later.
- **Skip:** Kubernetes, microservices, Redis, gRPC, an ORM, and billing.

## Settled after the stack (2026-10-04)

- **License:** MIT.
- **Roles:** `admin` (everything) and `user` (only their own bots). One hosting company per install, no reseller tiers.
- **Plans:** users hold plans (resource pools), assigned by an admin or created by a billing system via the API.
- **Disk quotas:** one fixed-size disk image per bot, loop-mounted as its volume.
- **Bot UIDs:** one Linux UID per bot.

## Where things are

- **Spec (approved):** `docs/spec-v0.md`. Build from it, in its milestone order.
- **Decision log:** `docs/decisions.md`.
