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
