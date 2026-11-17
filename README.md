# Bénévoles APE

A small web app that lets a parents' association (APE) fill volunteer shifts
for big events such as the *fête de l'école* or the *marché de Noël*. It works
like a simpler Doodle or Framadate, organised by **stand** and **time slot**.

- The organiser creates an event and gets a **secret admin link** (no account).
- They add stands ("Pêche aux canards", "Buvette"…) and generate slots in one
  go: *14:00 → 17:00, 1 h slots, 2 people* gives 3 slots and 6 volunteers.
- Parents open the public link and see:
  - an **overview grid** (stands × hours, red / orange / green), shown as a
    list grouped by hour on phones;
  - **one tab per stand**, with a badge showing how many people are still
    missing;
  - a **"Mes créneaux"** tab to review or cancel their own sign-ups.
- Parents sign up with a name and an e-mail or phone number. There is no
  account: the browser remembers them, and they get a personal link to manage
  their slots from another device.
- Admin tools:
  - CSV export (opens directly in French Excel);
  - a printable planning, one A4 page per stand;
  - adding a volunteer by hand;
  - duplicating last year's event;
  - erasing personal data after the event (GDPR).

The UI is in French. Everything ships as one Go binary with templates, CSS and
htmx embedded, and data is stored in a local SQLite file.

## Quick start

```sh
make run                 # http://localhost:8080, database ./benevole.db
make test                # unit + HTTP tests
make build               # ./benevole (static binary, CGO disabled)
make build-arm64         # e.g. for a Raspberry Pi 4/5
```

Requires Go ≥ 1.23. The SQLite driver (`modernc.org/sqlite`) is pure Go, so
no C toolchain is needed.

## Configuration

| Flag | Environment variable | Default | Purpose |
|---|---|---|---|
| `-addr` | `BENEVOLE_ADDR` | `:8080` | Listen address |
| `-db` | `BENEVOLE_DB` | `benevole.db` | SQLite file (created if missing) |
| `-base-url` | `BENEVOLE_BASE_URL` | *(from request)* | Public URL shown in shareable links |
| `-trust-proxy` | `BENEVOLE_TRUST_PROXY` | `false` | Trust `X-Forwarded-For/Proto` (behind Caddy/nginx) |
| — | `BENEVOLE_CREATE_PASSWORD` | *(none)* | If set, required to create an event |

`BENEVOLE_CREATE_PASSWORD` is recommended on a public server, so that only the
APE can create events.

## Deployment

1. Build the binary and copy it to the server (`/usr/local/bin/benevole`).
2. Install the [systemd unit](deploy/benevole.service) and adjust
   `BENEVOLE_BASE_URL`.
3. Put [Caddy](deploy/Caddyfile) in front for automatic HTTPS.

### Backups

The database is a single file in WAL mode. Take a consistent copy while the
server is running:

```sh
sqlite3 /var/lib/benevole/benevole.db ".backup '/var/backups/benevole-$(date +%F).db'"
```

For example from a daily cron job: `0 3 * * * benevole sqlite3 …`.

## How it works

```
cmd/benevole/          main: flags, graceful shutdown
internal/model/        domain types, display name, fill status, planning view builder
internal/slotgen/      splits a time range into slots
internal/store/        SQLite access, migrations (PRAGMA user_version), sign-up transaction
internal/web/          handlers, middleware, templates/, static/ (all embedded)
```

- **No overbooking.** A sign-up runs in a single `BEGIN IMMEDIATE`
  transaction that checks the capacity and inserts the row. A test runs 20
  concurrent sign-ups for the last places.
- **Secrets.**
  - Admin and volunteer links carry 256-bit random tokens.
  - Tokens are redacted from logs.
  - `Referrer-Policy: same-origin` keeps them from leaking to other sites.
  - Admin pages are sent with `Cache-Control: no-store`.
- **CSRF.** POSTs from another site are rejected using the `Sec-Fetch-Site` and
  `Origin` headers. Cookies are `SameSite=Lax`.
- **Abuse.**
  - POSTs are rate-limited per IP.
  - The sign-up form has a hidden honeypot field to catch bots.
  - The CSV export guards against formula injection.
- **Privacy.**
  - Public pages show only "Prénom N.".
  - E-mails and phone numbers are visible to the organisers only.
  - Pages are marked `noindex`.
