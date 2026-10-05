# Mechon v0 spec

Status: **approved** · 2026-10-04

This is the build plan for v0. Read [CLAUDE.md](../CLAUDE.md) first for the product and the decided stack; this document does not repeat the why. Decisions made here are logged in [decisions.md](decisions.md). Anything marked **(open)** needs a founder call before that part is built.

## 1. Scope

**v0 ships:** one operator installs the panel and one or more nodes, defines plans, creates customers (by hand or from a billing system through the API), and customers deploy Discord bots from a template by folder upload, git or API, then watch logs and metrics and start, stop or restart them. Every bot runs in its own locked-down container.

**v0 does not ship:** billing, HTTP ingress or domains for bots, multiple operators per install, an in-panel file editor, a CLI (the API is enough to script against; a CLI follows), agent templates (OpenClaw comes after v0), or gVisor by default (it is wired in as a per-plan flag).

## 2. Shape of the system

```
 browser ──HTTPS──▶  mechon (panel)  ◀──WSS (dialed by agent)──  mechon-agent ──▶ Docker Engine
 billing ──HTTPS──▶  • API + embedded web UI                      (one per node)    └─ one container per bot
                     • River jobs (deploys, webhooks, rollups)
                     • Postgres
```

- **Two binaries, one Go module:** `mechon` (panel) and `mechon-agent` (node). The agent does not carry the web UI.
- **Panel is stateless apart from Postgres** and a local artifact directory (S3-compatible storage is a later option behind an interface). One panel instance in v0. River and sessions in Postgres keep the door open for more.
- **The agent dials out to the panel.** Nodes need no inbound ports, which matters for operators running nodes behind NAT or on cheap VPSes.
- **Desired state lives in the panel, the agent converges.** The panel never issues "do X now and hope"; it sends the full spec of what each bot should be, and the agent reconciles. A dropped connection, an agent restart or a node reboot heals itself on reconnect.

### Repo layout

```
cmd/mechon/            panel main
cmd/mechon-agent/      agent main
internal/panel/        HTTP handlers, auth, scheduler, agent hub
internal/db/           migrations (goose, embedded) + sqlc output
internal/jobs/         River workers
internal/agent/        agent: connection, reconciler, stats, logs
internal/runtime/      Runtime interface + docker implementation
internal/proto/        panel ↔ agent message types (shared)
internal/templates/    built-in templates (embedded YAML)
web/                   React app; web/embed.go embeds web/dist
docs/
```

### Libraries

Kept deliberately short. Anything not listed needs a reason.

| Concern | Choice |
|---|---|
| HTTP routing | `net/http` (Go 1.22+ pattern mux). No framework. |
| Postgres | `jackc/pgx/v5` + `sqlc` |
| Migrations | `pressly/goose` with embedded SQL (sqlc reads the same files) |
| Jobs | `riverqueue/river` |
| WebSocket | `coder/websocket` |
| Passwords | `golang.org/x/crypto/argon2` (argon2id) |
| Docker | official Moby client |
| Git deploys | `go-git` (pure Go, keeps the panel a single binary with no `git` dependency) |
| Logging | `log/slog` |
| Web | React, Vite, TanStack Query, React Router, shadcn/ui, Tailwind 4, lucide |

Config is environment variables and flags only, no config library.

## 3. Data model

One install = one operator. Staff and customers are both `users`; the role decides what they can see.

```
users ─┬─ sessions
       ├─ api_keys
       └─ subscriptions ── plans
              └─ bots ── nodes
                   ├─ deploys
                   ├─ bot_env
                   └─ bot_metrics
webhook_endpoints ── webhook_deliveries (River does the retrying)
audit_log
```

### Tables (key columns only)

**users** · `id uuid`, `email citext unique`, `name`, `password_hash` (nullable for API-provisioned customers who have not set one), `role` enum `admin | user` (admins see and manage everything; users see only their own bots and account), `external_id` (the billing system's customer id, unique when set), `suspended_at`, `created_at`.

**plans** · `id`, `slug unique`, `name`, `max_bots`, `memory_mb`, `cpu_millicores`, `disk_mb`, `pids_max`, `hardened bool` (run under gVisor), `templates text[]` (which templates the plan allows; empty = all), `archived_at`. Limits are a **pool** shared by all bots on the subscription. A plan with `max_bots = 1` is the "one bot per order" product most hosts sell, so both models fit.

**subscriptions** · `id`, `user_id`, `plan_id`, `status` enum `active | suspended | terminated`, `external_id` (the billing system's service id), `created_at`. A customer can hold several.

**nodes** · `id`, `name`, `region`, `token_hash`, `memory_mb`, `cpu_millicores`, `disk_mb` (sellable capacity, set by the operator), `overcommit numeric` (default 1.0), `maintenance bool`, `agent_version`, `capabilities jsonb` (e.g. `runsc` available), `last_seen_at`.

**bots** · `id`, `uid_seq` (from a sequence; the bot's Linux UID is `100000 + uid_seq`), `subscription_id`, `node_id`, `name`, `template`, `memory_mb`, `cpu_millicores`, `disk_mb`, `desired_state` enum `running | stopped`, `observed_state` enum `pending | installing | running | stopped | crashed | unknown`, `current_deploy_id`, `restart_count`, `created_at`, `deleted_at`.

**deploys** · `id`, `bot_id`, `number` (per bot, 1..n), `source` enum `upload | git | api`, `git_url`, `git_ref`, `git_commit`, `artifact_sha256`, `artifact_bytes`, `status` enum `queued | fetching | installing | live | failed | superseded`, `error`, `created_by`, `created_at`, `finished_at`. A rollback is a new deploy row pointing at an older artifact, so history stays append-only.

**bot_env** · `bot_id`, `key`, `value_enc bytea` (AES-256-GCM, key from `MECHON_SECRET_KEY`), `secret bool` (secret values are write-only in the UI and API).

**bot_metrics** · `bot_id`, `ts` (minute bucket), `cpu_pct`, `memory_bytes`, `disk_bytes`, `net_rx`, `net_tx`. One row per bot per minute, kept 14 days by a River periodic job. The live view comes over the WebSocket and is not stored. At 1,000 bots that is about 20M rows, which plain Postgres with a `(bot_id, ts)` primary key handles fine.

**sessions** · `token_hash`, `user_id`, `expires_at`, `ip`, `user_agent`. **api_keys** · `id`, `user_id`, `name`, `prefix`, `hash`, `scopes text[]`, `last_used_at`, `expires_at`. **audit_log** · actor, action, target, metadata, timestamp.

### Quota enforcement

Every allocation runs in one transaction:

1. `SELECT … FROM subscriptions … FOR UPDATE` (serialises allocations per subscription).
2. Sum the limits of the subscription's live bots, add the new or changed bot, reject if any total exceeds the plan or `max_bots` is hit.
3. Place on a node: among nodes that are online, not in maintenance, allowed for the plan (`hardened` needs `runsc`), pick the one with the most free memory after `overcommit`. `SELECT … FOR UPDATE` on that node row.
4. Insert or update, commit, then tell the agent.

Limits are enforced twice: here (what the customer may ask for) and by the kernel through cgroups (what the process can actually use). Neither trusts the agent's own reporting.

## 4. Panel ↔ agent protocol

**Transport.** The agent connects to `wss://<panel>/agent/v1/connect` with `Authorization: Bearer <node token>`. Tokens are created in the panel when the operator adds a node (shown once, stored hashed). One connection per node; a second one replaces the first.

**Framing.** JSON text frames: `{"id": "…", "type": "…", "data": {…}}`. Requests carry an `id`; replies echo it with `type: "ok"` or `type: "error"`. Events have no `id`. Protocol version is in the URL path; the agent sends its version in `hello` and the panel refuses agents it is not compatible with.

**Liveness.** WebSocket ping every 15 s. The panel marks a node offline after 45 s without a pong and flags its bots `unknown` (it does **not** reschedule them: their data volume lives on that node).

### Agent → panel

| type | when | data |
|---|---|---|
| `hello` | on connect | agent version, Docker version, kernel, cgroup v2 yes/no, `runsc` available, total CPU, memory, disk, list of bot containers it currently has |
| `bot.state` | on every container state change | bot id, observed state, exit code, OOM-killed flag, restart count |
| `stats` | every 5 s, batched | per bot: CPU %, memory bytes, disk bytes, net rx/tx; plus node totals |
| `log` | while a subscription is open | bot id, stream (`stdout`/`stderr`/`system`), lines |
| `deploy.progress` | during a deploy | deploy id, phase, log lines |

### Panel → agent

| type | meaning |
|---|---|
| `sync` | the full list of bot specs that should exist on this node. Sent after `hello`. The agent removes anything Mechon-labelled that is not in the list. |
| `bot.apply` | create or update one bot to match a spec (below). Idempotent. |
| `bot.remove` | stop and delete the container and its volume. |
| `bot.restart` | restart without changing the spec. |
| `logs.subscribe` / `logs.unsubscribe` | start or stop streaming a bot's output; subscribe also returns the last N lines. |

A **bot spec** is everything the agent needs and nothing else: bot id, deploy id, template (image digest, install command, start command), limits (memory, CPU, pids, disk), `hardened`, env (decrypted by the panel just before sending, over TLS), desired state, and the artifact URL plus sha256.

**Artifacts.** The agent downloads `GET /agent/v1/artifacts/{deploy_id}` with its node token. The panel checks that the deploy's bot is placed on that node before serving. The agent checks the sha256 before extracting.

**Restarts.** The agent watches the Docker events stream. On an unexpected exit it reports `bot.state` and restarts with backoff (1 s, doubling to 5 min; reset after 10 min up). Docker's own restart policy is off so the panel always knows what happened.

## 5. The bot container

Created by the agent through the Docker Engine API.

| Setting | Value |
|---|---|
| Image | template image pinned by digest |
| User | the bot's own Linux UID/GID (`100000 + uid_seq`), never shared with another bot; the volume is owned by it |
| Root filesystem | read-only |
| Writable paths | `/home/container` (the bot's volume: a loop-mounted disk image, so its size is the quota) and `/tmp` (tmpfs, 64 MB, `noexec,nosuid`) |
| Memory | `Memory = MemorySwap = memory_mb` (no swap) |
| CPU | `NanoCPUs = cpu_millicores × 10⁶` |
| Processes | `PidsLimit = pids_max` (default 128) |
| Capabilities | `CapDrop: ALL`, none added |
| Security opts | `no-new-privileges`, default seccomp profile |
| ulimits | `nofile` 4096 |
| Network | per-node bridge `mechon0` with inter-container traffic disabled, no published ports. The agent installs `DOCKER-USER` iptables rules that drop traffic from `mechon0` to RFC 1918, link-local (cloud metadata at `169.254.169.254`) and the host. Public internet out is allowed; bots need the Discord gateway. |
| Logs | `local` driver, 10 MB × 3 files |
| Runtime | `runc`, or `runsc` (gVisor) when the plan is `hardened` |
| Restart policy | `no` (the agent supervises) |
| Labels | `mechon.bot`, `mechon.deploy` |

**Deploy on the node:**

1. Download and verify the artifact; extract into `/home/container/.mechon/next`.
2. Run the template's **install** command in a throwaway container with the same image, user, limits and volume (network on, read-only rootfs off for package managers, 10-minute timeout). Output streams back as `deploy.progress`.
3. On success, swap `next` into place, (re)create the bot container, start it if `desired_state` is `running`. On failure, leave the running version untouched and mark the deploy `failed`.

**Disk quota.** Each bot's volume is a fixed-size disk image (`MECHON_DATA_DIR/volumes/<bot>.img`, ext4, sparse) loop-mounted at `MECHON_DATA_DIR/mounts/<bot>` and bind-mounted into the container. A full image means the bot's writes fail and nobody else is affected. Works on any host filesystem. Resizing stops the bot, grows the image (`truncate` + `resize2fs`) and starts it again. Because images are sparse, the panel counts allocated (not used) disk against node capacity so operators cannot oversell disk by accident. XFS project quotas may come later as a faster option behind the same interface.

**Runtime interface.** Docker is behind it so calling crun directly stays possible later.

```go
type Runtime interface {
    Apply(ctx context.Context, spec BotSpec) error          // converge one bot
    Remove(ctx context.Context, botID string) error
    Restart(ctx context.Context, botID string) error
    List(ctx context.Context) ([]Instance, error)           // for sync
    Logs(ctx context.Context, botID string, tail int, follow bool) (io.ReadCloser, error)
    Stats(ctx context.Context) ([]Stats, error)
    Events(ctx context.Context) (<-chan Event, error)
}
```

## 6. Templates

Built-in, defined in embedded YAML so adding one is a file, not code. Each has: id, name, image (digest), install command, start command, the files it expects, and an env schema (e.g. `DISCORD_TOKEN`, required, secret).

| id | image | install | start |
|---|---|---|---|
| `discord-js` | `node:22-bookworm-slim` | `npm ci --omit=dev` (falls back to `npm install` without a lockfile) | `node ${MAIN:-index.js}` |
| `discord-py` | `python:3.12-slim` | `python -m venv .venv && .venv/bin/pip install -r requirements.txt` | `.venv/bin/python ${MAIN:-main.py}` |
| `bun` | `oven/bun:1` | `bun install --production` | `bun run ${MAIN:-index.ts}` |

OpenClaw and other agent templates come after v0, through the same mechanism.

## 7. Deploys

- **Upload:** the browser or API sends a `.tar.gz` or `.zip` (size capped at the bot's disk quota). The panel stores it by sha256 in `MECHON_DATA_DIR/artifacts`.
- **Git:** URL + ref (+ optional deploy key, stored encrypted). A River job shallow-clones with go-git, packs a tarball and continues as an upload.
- **API:** the upload endpoint with an API key, for CI.

Each creates a `deploys` row, then a River job pushes `bot.apply` to the agent and tracks progress. One deploy per bot runs at a time; a newer one supersedes a queued one.

## 8. Auth and API

- **Sessions:** a random 32-byte token in an `HttpOnly; Secure; SameSite=Lax` cookie, stored hashed, 30-day sliding expiry. Mutating requests also require the `Origin` header to match `MECHON_PUBLIC_URL`.
- **Passwords:** argon2id (64 MB, 3 iterations, 2 lanes); login rate-limited per IP and per account.
- **API keys:** `mk_` + 32 random bytes; shown once, stored as a sha256 hash with a visible prefix; scoped (`bots:read`, `bots:write`, `deploys:write`, and for staff `operator`).
- **One API** at `/api/v1`, used by the web UI and by everyone else. What a caller sees is decided by role and scope, not by separate endpoints.
- **Operator endpoints** for billing systems (Paymenter, WHMCS, Stripe glue): create or update a customer (`external_id`), create a subscription on a plan, suspend, unsuspend, terminate, change plan. Suspending sets every bot's `desired_state` to `stopped` and blocks starts. Terminating deletes bots after a grace period (a River job).
- **Webhooks:** endpoints with a secret and event filter. Events: `bot.created`, `bot.deleted`, `bot.crashed`, `deploy.live`, `deploy.failed`, `subscription.suspended`, `node.offline`. Signed with `Mechon-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">`. Delivered and retried by River (exponential backoff, 24 h).

## 9. Web panel

Static React app embedded in `mechon`; the Go server serves `index.html` for unknown non-API paths.

**User:** bots list → bot page (live console, metrics, deploys with rollback, env and secrets, settings, delete) · new bot (pick a template, size within the remaining pool, upload or git) · account (password, API keys).

**Admin:** overview (nodes, capacity, bots by state, recent crashes) · nodes (add a node and get its install command, capacity, maintenance) · plans · customers and subscriptions · webhooks · audit log · install settings.

**Look:** follow `~/GitHub/offerwall-site`, mapping its tokens onto shadcn's CSS variables so every shadcn component inherits them:

- near-black/warm-white surfaces in OKLCH, one violet accent (`oklch(0.62 0.22 300)` dark), orange as the secondary tone, muted red for danger; no gradients; dark by default with a light toggle;
- Bricolage Grotesque for headings and numbers, Geist for UI text, Geist Mono for logs and ids;
- 20 px card radius, pill buttons, the inverted-foreground active state in the sidebar, the uppercase section labels, the stat tile with a sparkline;
- the frosted `glass` surface for the login card and the command palette (⌘K);
- motion only where it carries meaning (live state changes, log arrival), and it respects `prefers-reduced-motion`.

Live data (console, state, stats) reaches the browser over one SSE stream per open bot page, fed from the agent hub. TanStack Query handles everything else.

## 10. Install

- **Panel:** one binary + Postgres. `MECHON_DATABASE_URL`, `MECHON_PUBLIC_URL`, `MECHON_SECRET_KEY`, `MECHON_DATA_DIR`, `MECHON_LISTEN`. Migrations run on start. `mechon init` creates the owner account.
- **Node:** Docker Engine + one binary. The panel's "add node" screen prints a one-liner that installs `mechon-agent` as a systemd unit with `MECHON_PANEL_URL` and `MECHON_NODE_TOKEN`.

## 11. Build order

| # | Milestone | Done when |
|---|---|---|
| 1 | Skeleton | `mechon` serves the embedded UI, runs migrations, owner can log in |
| 2 | Tenancy | plans, customers, subscriptions, quota checks, API keys, operator endpoints |
| 3 | Nodes | agent connects, `hello` / `sync` / heartbeat, node page shows live capacity |
| 4 | Run a bot | Docker runtime with the full container config, start/stop/restart, live logs and stats |
| 5 | Deploys | upload, git and API deploys through the install step; rollback |
| 6 | Hardening | egress rules, gVisor tier, disk quotas, webhooks, rate limits, audit log |

Each milestone ends with an integration test against real Postgres and, from milestone 3, a real Docker daemon. Milestone 6 adds an isolation test suite: from inside one bot, try to read another bot's files, signal its processes, reach it or the host over the network, and exceed memory, pids and disk. All of that must fail.

## 12. Open decisions

1. ~~License~~ **MIT** (decided 2026-10-04).
2. ~~Disk quotas~~ **one disk image per bot** (decided 2026-10-04).
3. ~~Bot UIDs~~ **one Linux UID per bot** (decided 2026-10-04).
4. ~~Operators~~ **one hosting company per install**, roles `admin` and `user`, users hold plans (decided 2026-10-04).

Nothing is open. Build starts at milestone 1.
