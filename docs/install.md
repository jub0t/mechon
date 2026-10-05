# Installing Mechon

A Mechon install has two parts:

- **The panel** (`mechon`): one binary with the API and the web UI, backed by Postgres.
- **Nodes** (`mechon-agent`): one agent per server that runs bots. The agent runs as root next to Docker and dials out to the panel, so nodes need no inbound ports.

The panel and a node can share a server, and small installs often start that way.

## The panel

### Requirements

- A Linux server (amd64 or arm64) with systemd.
- Postgres 15 or newer, on the same server or reachable from it.
- A domain such as `panel.example.com`, with TLS from a reverse proxy in front of the panel.

### 1. Download the binary

Pick a version from the [releases page](https://github.com/jub0t/mechon/releases).

```bash
VERSION=0.1.0                      # without the leading "v"
ARCH=amd64                         # or arm64
cd /tmp
curl -fsSLO https://github.com/jub0t/mechon/releases/download/v$VERSION/mechon_${VERSION}_linux_$ARCH.tar.gz
curl -fsSLO https://github.com/jub0t/mechon/releases/download/v$VERSION/checksums.txt
sha256sum --check --ignore-missing checksums.txt
tar -xzf mechon_${VERSION}_linux_$ARCH.tar.gz mechon
sudo install -m 0755 mechon /usr/local/bin/mechon
```

### 2. Create a system user and the database

The panel connects over the local Postgres socket as the `mechon` role, so there is no database password to manage.

```bash
sudo useradd --system --home-dir /var/lib/mechon-panel --shell /usr/sbin/nologin mechon
sudo -u postgres createuser mechon
sudo -u postgres createdb --owner mechon mechon
```

If Postgres runs on another host, create a role with a password instead and use a URL like `postgres://mechon:PASSWORD@db.internal:5432/mechon?sslmode=require`.

### 3. Write the config

Generate the secret key that encrypts bot secrets at rest:

```bash
mechon keygen
```

Create `/etc/mechon/panel.env`:

```bash
MECHON_DATABASE_URL=postgres:///mechon?host=/var/run/postgresql
MECHON_PUBLIC_URL=https://panel.example.com
MECHON_SECRET_KEY=paste-the-keygen-output-here
MECHON_DATA_DIR=/var/lib/mechon-panel
MECHON_LISTEN=127.0.0.1:8080
MECHON_TRUST_PROXY=true
```

Use `MECHON_DATA_DIR=/var/lib/mechon-panel` rather than `/var/lib/mechon`, because the agent uses the latter by default if a node runs on the same server. Lock the file down:

```bash
sudo chown root:mechon /etc/mechon/panel.env
sudo chmod 0640 /etc/mechon/panel.env
```

### 4. Run it with systemd

Create `/etc/systemd/system/mechon.service`:

```ini
[Unit]
Description=Mechon panel
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=mechon
Group=mechon
EnvironmentFile=/etc/mechon/panel.env
ExecStart=/usr/local/bin/mechon serve
StateDirectory=mechon-panel
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now mechon
journalctl -u mechon -f            # should end with "panel listening"
```

The panel runs its database migrations each time it starts.

### 5. Put a reverse proxy in front

With [Caddy](https://caddyserver.com), which fetches the TLS certificate itself, `/etc/caddy/Caddyfile` is:

```caddy
panel.example.com {
	reverse_proxy 127.0.0.1:8080 {
		# Flush at once so live logs (SSE) and the agent WebSocket stream without delay.
		flush_interval -1
	}
}
```

Any other proxy works if it does two things:

- **Passes WebSocket upgrades on `/agent/v1/connect`.** Nodes connect there.
- **Does not buffer server-sent events on `/api/v1/bots/*/stream`.** Live logs and stats use them. Caddy handles both by default. With nginx, add:

```nginx
location ~ ^/api/v1/bots/[^/]+/stream$ {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
location = /agent/v1/connect {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 1h;
}
```

In nginx, also set `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` and `proxy_set_header Host $host;` on every location, and raise `client_max_body_size` to your largest bot upload.

### 6. Create the first admin

```bash
sudo -u mechon sh -c 'set -a; . /etc/mechon/panel.env; exec mechon init --email you@example.com --name "Your Name"'
```

It prompts for the password. In a script, pipe the password in and add `--password-stdin`. Then open `https://panel.example.com` and sign in.

## Nodes

### Requirements

- Linux (amd64 or arm64) with systemd, and root access.
- [Docker Engine](https://docs.docker.com/engine/install/), installed and running.
- `losetup`, `mkfs.ext4`, `resize2fs` and `iptables`. On Debian or Ubuntu: `apt-get install -y util-linux e2fsprogs iptables`.
- Outbound HTTPS to the panel's URL.

### Add a node

1. In the panel, open **Nodes**, then **Add node**. Copy the node token; the panel shows it only once.
2. On the node, run:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/jub0t/mechon/main/scripts/install-agent.sh \
     | sudo MECHON_PANEL_URL=https://panel.example.com MECHON_NODE_TOKEN=paste-the-token sh
   ```

   The script checks the host, downloads the latest release and verifies its checksum, installs `/usr/local/bin/mechon-agent`, writes `/etc/mechon/agent.env` (mode 0600) and starts the `mechon-agent` systemd unit. It does not install Docker for you. To pin a version, add `MECHON_VERSION=v0.1.0`. The optional settings `MECHON_DATA_DIR`, `MECHON_DNS` and `MECHON_LOG_LEVEL` are passed the same way.
3. Within a few seconds the node shows as **online** on the Nodes page. If it doesn't, check `journalctl -u mechon-agent -f` on the node.

## Upgrading

**Panel.** Back up the database first (see below), repeat step 1 with the new version, then run `sudo systemctl restart mechon`. Migrations run on start.

**Nodes.** Re-run the install command without any variables. It installs the latest release, keeps `/etc/mechon/agent.env` and restarts the agent:

```bash
curl -fsSL https://raw.githubusercontent.com/jub0t/mechon/main/scripts/install-agent.sh | sudo sh
```

Bots keep running while the agent restarts, and the new agent takes them over. Upgrade the panel before the nodes.

## Backups

| What | Where | How |
|---|---|---|
| Database | Postgres | `sudo -u mechon pg_dump -Fc mechon > mechon-$(date +%F).dump` |
| Uploaded bot code | `$MECHON_DATA_DIR/artifacts` on the panel | Copy or `tar` the directory |
| Config and secret key | `/etc/mechon/panel.env` | Store a copy somewhere safe and separate |
| Bot data | `/var/lib/mechon/volumes/*.img` on each node | Copy the disk images, ideally with the bot stopped |

To restore, load the dump with `pg_restore --clean --dbname mechon`, put the artifacts back in place and start the panel with the **same** `MECHON_SECRET_KEY`.

## Security notes

- **Keep `MECHON_SECRET_KEY` safe and backed up.** It encrypts bot secrets such as Discord tokens. If you lose it, those secrets can't be read and every customer has to enter them again. If it leaks, an attacker with a database copy can read them.
- `/etc/mechon/panel.env` and `/etc/mechon/agent.env` hold credentials. Keep them readable only by root (and the `mechon` group for the panel).
- A node token lets its holder act as that node. Treat it like a password. If one leaks, issue a new token for that node in the panel and re-run the install command with it (`MECHON_NODE_TOKEN=... sh`).
- Bind the panel to `127.0.0.1` and reach it only through the TLS proxy. Set `MECHON_TRUST_PROXY=true` only when a proxy is in front, because otherwise clients can spoof their IP.
- Nodes need no open inbound ports for Mechon. Firewall them as you would any server.
