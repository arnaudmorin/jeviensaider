# Je viens aider

[![build](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml/badge.svg?branch=master)](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml)

A small web app that helps a parents' association (APE) find volunteers for
school events such as the *fête de l'école* or the *marché de Noël*.

It works like a simpler Doodle or Framadate, organised by **stand** and
**time slot**. For example, *Pêche aux canards* from 14:00 to 17:00 with
2 people per hour gives 3 slots and 6 places to fill.

The interface is in French. The whole app is one Go binary, and its data lives
in a single SQLite file.

## Features

**For organisers**
- Create an event and get a **secret admin link**, with no account to create.
- Add stands, then generate their slots in one step: start, end, slot length
  and number of people.
- Follow progress on a dashboard that shows who is signed up, with their
  contact details, and how many places are still missing.
- Open and close sign-ups.
- Add a volunteer by hand.
- Export to CSV; the file opens directly in French Excel.
- Print the planning, one A4 page per stand.
- Duplicate an event to reuse it next year.
- Erase personal data after the event (GDPR). Whole events are also deleted
  automatically 60 days after they end, and the admin page shows the date.

**For parents**
- An **overview grid** (stands × hours, coloured red / orange / green),
  shown as a list grouped by hour on phones.
- **One tab per stand**, with a badge showing how many people are still
  missing.
- Sign up with a name and an e-mail or phone number. There is no account:
  the browser remembers them, and a personal link lets them manage their slots
  from another device.
- A **"Mes créneaux"** tab, with a warning when two of their slots overlap.

## Quick start

Requires Go ≥ 1.23. No C toolchain is needed: the SQLite driver is pure Go.

```sh
make run      # http://localhost:8080, database ./benevole.db
make test     # unit + HTTP tests
make build    # ./benevole, a static binary
```

Open http://localhost:8080, create an event, and keep the admin link it gives
you.

Prebuilt binaries for `linux-amd64`, `linux-arm64` (Raspberry Pi 4/5) and
`linux-armv7` are attached to every
[build on `master`](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml)
as downloadable artifacts. They can also be built locally with
`make build-arm64` or `make build-armv7`.

## Configuration

| Flag | Environment variable | Default | Purpose |
|---|---|---|---|
| `-addr` | `BENEVOLE_ADDR` | `:8080` | Listen address |
| `-db` | `BENEVOLE_DB` | `benevole.db` | SQLite file, created if missing |
| `-base-url` | `BENEVOLE_BASE_URL` | *(from request)* | Public URL used in shareable links, e.g. `https://jeviensaider.fr` |
| `-trust-proxy` | `BENEVOLE_TRUST_PROXY` | `false` | Trust `X-Forwarded-*` headers when behind Caddy or nginx |
| `-retention-days` | `BENEVOLE_RETENTION_DAYS` | `60` | Delete events this many days after they end; `0` keeps them forever |
| — | `BENEVOLE_CREATE_PASSWORD` | *(none)* | Password required to create an event |

On a public server, set `BENEVOLE_CREATE_PASSWORD` so that only the APE can
create events. It is read from the environment only, so it never shows up in
`ps` output.

## Docker

```sh
cp .env.example .env          # set BENEVOLE_BASE_URL and BENEVOLE_CREATE_PASSWORD
docker compose pull && docker compose up -d   # prebuilt image
# or: docker compose up -d --build            # build it locally
```

Every push to `master` publishes a multi-arch image (amd64, arm64, armv7) to
`ghcr.io/arnaudmorin/jeviensaider`. It is tagged `latest` and with the
commit's short SHA (`sha-xxxxxxx`) for pinning a version.

- The app listens on `127.0.0.1:8080` (change `PORT` in `.env`). Put a reverse
  proxy in front of it for HTTPS.
- The database is stored in the `data` volume, at `/data/benevole.db`.
- The image is small (about 15 MB) and based on distroless. It runs as a non-root user
  with a read-only root filesystem, and has a built-in health check
  (`benevole -healthcheck`).

To back up the database from the volume:

```sh
docker compose cp app:/data/benevole.db ./backup-$(date +%F).db
```

Stop the app first (`docker compose stop`) to be sure the copy is consistent,
or use the `sqlite3 .backup` command below on the host.

## Deployment without Docker

1. Copy the binary to the server as `/usr/local/bin/benevole`.
2. Install the [systemd unit](deploy/benevole.service) and set
   `BENEVOLE_BASE_URL` in it.
3. Put [Caddy](deploy/Caddyfile) in front of the app for automatic HTTPS.

**Backups.** The database is a single file in WAL mode. This command takes a
consistent copy while the server is running, for example from a daily cron job:

```sh
sqlite3 /var/lib/benevole/benevole.db ".backup '/var/backups/benevole-$(date +%F).db'"
```

## Under the hood

```
cmd/benevole/      entry point: flags, graceful shutdown
internal/model/    domain types, fill status, planning view
internal/slotgen/  splits a time range into slots
internal/store/    SQLite access, embedded migrations, sign-up transaction
internal/web/      handlers, middleware, embedded templates and static files (htmx)
```

- **No overbooking.** Each sign-up checks capacity and inserts the row in a
  single `BEGIN IMMEDIATE` transaction. A test runs 20 concurrent sign-ups for
  the last places.
- **Secret links.** Admin and personal links carry 256-bit random tokens.
  Tokens are redacted from logs, `Referrer-Policy: same-origin` stops them
  leaking to other sites, and admin pages are never cached.
- **Protection.**
  - Cross-site POSTs are rejected using the `Sec-Fetch-Site` and `Origin`
    headers.
  - POSTs are rate-limited per IP.
  - The sign-up form has a honeypot field to catch bots.
  - A strict CSP is sent.
  - The CSV export guards against formula injection.
- **Automatic cleanup.** At startup and then every hour, the app deletes
  every event whose last day is more than `BENEVOLE_RETENTION_DAYS` days ago,
  together with its stands, slots and volunteers. An event's last day is the
  latest of its date, its last slot and its creation date, so an event that
  is still being prepared is never deleted.
- **Privacy.** Public pages show only "Prénom N.". E-mails and phone numbers
  are visible to the organisers only, and pages are marked `noindex`.

## Continuous integration

The [GitHub Actions workflow](.github/workflows/build.yml) runs on every push
or pull request to `master`. It checks `gofmt`, runs `go vet` and the tests
with the race detector, then builds the three Linux binaries and the Docker
image. The image is pushed to GitHub's container registry only for pushes to
`master`; pull requests just build it.
