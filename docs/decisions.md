# Decisions

Newest last. Status is **Decided** (founder call), **Proposed** (in the spec, awaiting the founder), or **Open**.

| Date | Decision | Status |
|---|---|---|
| 2026-10-04 | Greenfield rewrite; old Rust code deleted, nothing carried over except the repo itself | Decided |
| 2026-10-04 | Product: self-hosted multi-tenant bot hosting panel for hosting operators, Discord bots first, agents later | Decided |
| 2026-10-04 | Stack: Go (panel + agent), Postgres with sqlc and River, no Redis, React + Vite + TanStack Query + shadcn/ui embedded in the Go binary, Docker Engine API for isolation with gVisor as a hardened tier, behind a `Runtime` interface. No Next.js for the panel. No billing | Decided |
| 2026-10-04 | Two binaries (`mechon`, `mechon-agent`) in one Go module | Proposed |
| 2026-10-04 | Declarative panel → agent protocol: the panel sends full bot specs, the agent reconciles; `sync` on every connect | Proposed |
| 2026-10-04 | Users get plans (admin-assigned or created by a billing system); a plan is a resource pool per subscription, and `max_bots = 1` covers the one-bot-per-order product | Decided |
| 2026-10-04 | Bots are not rescheduled when a node goes offline (their volume lives there); they show `unknown` | Proposed |
| 2026-10-04 | Agent supervises restarts with backoff; Docker restart policy off | Proposed |
| 2026-10-04 | Per-minute metrics rollups in Postgres, 14-day retention; live stats are streamed, not stored | Proposed |
| 2026-10-04 | `net/http` mux (not chi), goose migrations, coder/websocket, go-git for git deploys | Proposed |
| 2026-10-04 | UI follows offerwall-site: its tokens mapped onto shadcn variables, Bricolage + Geist, dark default | Proposed |
| 2026-10-04 | One install = one hosting company (no reseller tiers) | Decided |
| 2026-10-04 | Two roles: `admin` (everything) and `user` (only their own bots) | Decided |
| 2026-10-04 | License: MIT | Decided |
| 2026-10-04 | Disk quotas: one fixed-size disk-image file per bot, loop-mounted as its volume (works on any filesystem). XFS project quotas may come later as a faster option | Decided |
| 2026-10-04 | One Linux UID per bot (`100000 + bots.uid_seq`), so a container escape still cannot read other bots' files | Decided |
| 2026-10-04 | The agent talks to the Docker Engine API directly over the unix socket with net/http (API v1.44, ~14 endpoints, its own log demux) instead of the Moby client, to keep the agent small | Decided |
| 2026-10-04 | Install containers run on `mechon0` too, so customer install scripts (npm postinstall etc.) are under the same egress firewall as the bot | Decided |
| 2026-10-04 | Bots use public DNS resolvers (`MECHON_DNS`, default 1.1.1.1 and 8.8.8.8) so blocking private ranges does not break DNS; a private resolver gets a port-53-only hole | Decided |
| 2026-10-04 | Bot containers also get `Init: true` (tini), `IpcMode: private` and `nodev` on /tmp; the firewall additionally drops 0/8, 127/8, multicast, 240/4 and 198.18/15 | Decided |
| 2026-10-04 | Disk images grow online (`truncate`, `losetup -c`, `resize2fs`); shrinking is refused | Decided |
| 2026-10-04 | River is deferred until webhooks: desired-state reconciliation makes deploy dispatch durable without a job queue | Decided |
| 2026-10-04 | Admins can override any plan limit (bots, memory, CPU, disk, processes) for one customer's subscription, with a note; empty means "use the plan" | Decided (founder) |
| 2026-10-04 | Webhooks deliver through River (Postgres-backed): one delivery row per endpoint per event, Stripe-style HMAC-SHA256 signature, 14 attempts with exponential backoff (about a day) | Decided |
| 2026-10-04 | File manager in the panel: browse and edit a bot's app/ and data/ through the agent, confined with os.Root and never following symlinks; edits under app/ are overwritten by the next deploy | Decided (founder) |
