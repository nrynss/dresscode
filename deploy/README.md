# Deploy on this machine

Dresscode runs as two systemd **user** units next to the machine's Lambo
unit (`lambo-dresscode.service`), plus a nightly backup timer. No root is
needed anywhere: the tunnel this deploy uses is the user-level one, its
config and credentials live in `~/.cloudflared/`, and the other (system)
tunnel on the machine is never touched.

## What runs where

| Thing | Where |
|---|---|
| Runtime directory | `/home/nryn/dresscode-run/` |
| Environment file (keys, mode 600) | `/home/nryn/dresscode-run/env` |
| Settings | `/home/nryn/dresscode-run/dresscode.toml` |
| SQLite store + data directories | `/home/nryn/dresscode-run/data/` |
| Built app | `/home/nryn/dresscode-run/web/dist/` |
| Binary | `~/.local/bin/dresscode` |
| Tunnel config | `~/.cloudflared/config-dresscode.yml` |
| Installed units | `~/.config/systemd/user/dresscode*.service`, `dresscode-backup.timer` |
| Backups | `/home/nryn/dresscode-backups/<timestamp>/` |
| Public URL | `https://dresscode.nryn.dev` → `127.0.0.1:8091` |

The running service reads **only** the runtime directory — nothing points
back into a source checkout.

## Install, in order

From a checkout of this repository:

```sh
# 1. Build the binary and the app.
go build -o ~/.local/bin/dresscode ./cmd/dresscode
(cd web && pnpm build)          # output lands in web/build
rm -rf /home/nryn/dresscode-run/web/dist
mkdir -p /home/nryn/dresscode-run/web
cp -a web/build /home/nryn/dresscode-run/web/dist

# 2. Runtime directory: env, settings, data.
install -d -m 700 /home/nryn/dresscode-run
umask 077
cp <path-to-your-.env> /home/nryn/dresscode-run/env
printf 'DRESSCODE_SIGNING_KEY=%s\n' "$(openssl rand -base64 32)" >> /home/nryn/dresscode-run/env
printf 'DRESSCODE_CODE_KEY=%s\n'    "$(openssl rand -base64 32)" >> /home/nryn/dresscode-run/env
chmod 600 /home/nryn/dresscode-run/env
umask 022
sed -e 's|^occasions = .*|occasions = "/home/nryn/dresscode-run/data/occasions"|' \
    -e 's|^samples = .*|samples = "/home/nryn/dresscode-run/data/samples"|' \
    dresscode.toml.example > /home/nryn/dresscode-run/dresscode.toml
chmod 600 /home/nryn/dresscode-run/dresscode.toml
mkdir -p /home/nryn/dresscode-run/data
cp -a testdata/occasions testdata/samples testdata/youcam /home/nryn/dresscode-run/data/
# The samples mount reads the sibling directory <samples>/../youcam/results,
# which is why youcam/ is copied beside samples/.

# 3. Scripts where the units look for them.
install -d -m 755 /home/nryn/dresscode-run/bin
install -m 755 deploy/backup deploy/health-check /home/nryn/dresscode-run/bin/

# 4. DNS: point the hostname at the Dresscode tunnel (one-time; uses the
#    zone certificate ~/.cloudflared/cert.pem).
cloudflared tunnel route dns 7f69d01b-2983-4191-aed3-030764372497 dresscode.nryn.dev

# 5. Tunnel config and units.
cp deploy/config-dresscode.yml ~/.cloudflared/config-dresscode.yml
install -m 644 deploy/dresscode.service deploy/dresscode-tunnel.service \
    deploy/dresscode-backup.service deploy/dresscode-backup.timer \
    ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now dresscode.service dresscode-tunnel.service dresscode-backup.timer

# 6. Prove it.
deploy/health-check
```

Linger must be on for the units to come back at boot
(`loginctl show-user "$USER" -p Linger` — this machine already has it).
`WantedBy=default.target` plus enabled units plus linger is the whole
reboot story: every unit above starts again without anyone signing in.

## Updating the deployed app or binary

Rebuild (`go build` and `pnpm build`), copy the outputs into the runtime
directory as in steps 1, then `systemctl --user restart dresscode.service`.
No other step changes.

## Health check

`deploy/health-check [public-url] [local-url]` curls the public URL and
the local port for the app's HTML and the samples list. It is free by
construction: the samples route reads recorded data and dials no
provider. Nonzero exit on any wrong answer.

## Backup

`deploy/backup` (installed as `/home/nryn/dresscode-run/bin/backup`) does
one online SQLite `.backup` of the store plus everything beside it in the
data directory — the media blobs (Keel's mediastore keeps them as files
under `data/media`), the upload staging area, the occasions, the samples
and the recorded results — into
`/home/nryn/dresscode-backups/<timestamp>/`. `dresscode-backup.timer`
runs it nightly. The environment file is never backed up: it holds the
keys.

## Rollback

- Stop the deploy: `systemctl --user disable --now dresscode.service dresscode-tunnel.service dresscode-backup.timer`. Nothing else on the machine changes; the other tunnel is a separate process on a separate config.
- Restore data: stop `dresscode.service`, copy a backup's `dresscode.db` over `/home/nryn/dresscode-run/data/dresscode.db` and its `media/` over `/home/nryn/dresscode-run/data/media/`, start the unit again.
- Retire the hostname: `cloudflared tunnel route ip delete dresscode.nryn.dev` (or delete the DNS record in the Cloudflare dashboard).
- Remove everything: the table at the top is the inventory — delete the runtime directory, `~/.cloudflared/config-dresscode.yml`, the installed units, and the backups directory.
