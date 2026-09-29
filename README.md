# Quick Vote

A small, self-hostable web app for deciding between options (board games, movies,
lunch spots — anything) using **quadratic voting**. One person creates a vote and
shares a link; everyone suggests options, votes with a shared credit budget, and
sees a scored result with veto, tiebreak, and re-vote mechanics.

It ships as a **single container**: a Go binary that embeds a React SPA and keeps
all state in one SQLite file. One port, one volume, no external services.

## Quick start

```sh
docker compose up -d
```

Then open <http://localhost:8080>, create a vote, and share the room link
(`/v/<slug>`) with your group. State lives in the `quickvote-data` volume
(`/data/quickvote.db` inside the container).

To run the image directly instead of Compose:

```sh
docker build -t quickvote .
docker run -d -p 8080:8080 -v quickvote-data:/data quickvote
```

## How it works

Three phases move in order: **suggesting → voting → results**.

1. **Suggesting** — participants add options (subject to a per-user cap).
   Anyone can mark themselves "done suggesting" at any point (even with zero
   suggestions) — it doesn't stop them adding or deleting more afterwards, but
   feeds the `all-done` advance rule below.
2. **Voting** — everyone gets a shared budget of `credits per option × number of
   options`. Casting `v` votes on one option costs `v` raised to the vote's
   cost-scaling exponent (2 = quadratic by default, so spreading support is
   cheap and piling onto one option is expensive; 1 = linear; up to 4 for a
   much steeper penalty). You can resubmit your ballot until the phase ends.
   If vetoes are enabled, a voter can spend a flat credit cost to explicitly
   veto an option instead of allocating credits to it.
3. **Results** — each option's score is the sum of votes cast on it. Options
   below the survival threshold, or explicitly vetoed by anyone, are
   eliminated; the highest surviving score wins, with ties resolved by the
   configured tiebreaker. The `runoff` tiebreaker instead reopens voting on
   just the tied options (ballots cleared) and resolves a second tie with its
   fallback rule, guaranteeing an outcome after one extra round. Anyone can
   call for a re-vote; once enough people do, ballots clear and the room
   returns to voting.

Phase changes and every other update are pushed live over a WebSocket, so all
open browsers stay in sync.

Votes are stored in SQLite and deleted automatically after 90 days without activity
(configurable with `QV_RETENTION_DAYS`; `0` keeps them forever). Until then a
vote's room stays reachable at its `/v/<slug>` link, and each browser also
keeps a local "recent votes" list (in `localStorage`, not synced anywhere) so
you can find your way back to rooms you created or joined without keeping the
link.

## Settings glossary

Set at creation time (the last few live behind an "Advanced" fold):

| Setting | Default | Meaning |
|---|---|---|
| **Title** | required | Name of the vote, e.g. "Friday game night". |
| **Max suggestions per user** | 3 | Cap on how many options each participant may add (1–20). |
| **Credits per option** | 3 | Budget multiplier. Total budget = this × number of options. Higher = more expressive ballots. |
| **Suggestion-phase advance rule** | manual | `manual` (creator advances), `count:N` (N distinct participants have each submitted at least one suggestion), `suggestion-count:N` (N total suggestions submitted, by anyone), or `all-done` (every current participant has marked themselves "done suggesting"). |
| **Voting-phase advance rule** | all-voted | `manual` or `all-voted` (auto-advance once every current participant has submitted a ballot). |
| **Survival threshold** | 0 | Minimum total score an option needs to survive. The default `0` disables the threshold, so every non-vetoed option stays in contention. Set `1` to eliminate options nobody spent credits on; raise it further to require broader support. |
| **Veto cost** | 0 (off) | Credits a voter spends to explicitly veto an option (ballot value `-1`) instead of allocating credits to it. `0` disables vetoing. A vetoed option is eliminated no matter its score; results show "vetoed by N voter(s)", distinct from an under-threshold elimination. |
| **Vote cost scaling** | 2.0 (quadratic) | Exponent in the cost formula `ceil(votes ^ exponent)`, 1.0–4.0. `1` = linear cost, `2` = the original quadratic cost, higher values penalize concentrating credits on one option more steeply. |
| **Tiebreaker** | most-backers | How a tie for the top score is broken: `most-backers` (most distinct voters, then random), `random`, `creator` (results pause and the creator picks among the tied options), `earliest` (earliest-suggested tied option wins), or `runoff` (reopen voting on just the tied options, ballots cleared; a repeat tie resolves via the fallback below). |
| **Runoff fallback** | random | Only used when tiebreaker is `runoff`: how a tie *within* the runoff round itself is resolved (`most-backers`, `random`, `creator`, or `earliest` — never another runoff, so it always terminates). |
| **Re-vote threshold** | 33% | Percentage of current participants whose re-vote calls are needed to send the room back to voting (rounded up, minimum 1). |
| **Suggestion timer** | off | Optional duration; when it expires the suggestion phase auto-advances (held if fewer than 2 options exist). |
| **Voting timer** | off | Optional duration; when it expires the voting phase auto-advances and results are scored. |

The creator is a participant like everyone else and holds a separate creator
token (kept in the browser) that gates the "advance phase" and creator-tiebreak
controls. Settings cannot be changed after a vote is created.

## Development

Backend (Go 1.23+; the Docker build uses 1.27):

```sh
go run ./cmd/quickvote -addr :8080 -db ./quickvote.db
```

Frontend (Node 22+), with a dev server that proxies `/api` (including the
WebSocket) to the Go backend on `:8080`. WebSockets are same-origin only, so
start the backend with the dev server's origin allowed:

```sh
QV_ALLOWED_ORIGINS=http://localhost:5173 go run ./cmd/quickvote -db ./quickvote.db
cd web
npm install
npm run dev
```

Run the tests:

```sh
go test ./...        # backend: domain, store, and full-lifecycle API tests
cd web && npm test   # frontend: budget-math unit tests (Vitest)
```

### Configuration

Flags (with environment-variable fallbacks):

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `-addr` | `QV_ADDR` | `:8080` | Listen address. |
| `-db` | `QV_DB` | `/data/quickvote.db` | SQLite database file path (its directory is created if missing). |
| `-retention-days` | `QV_RETENTION_DAYS` | `90` | Delete votes after this many days without activity (checked at startup and daily). `0` disables. |
| `-trusted-ip-header` | `QV_TRUSTED_IP_HEADER` | *(none)* | Header carrying the real client IP from your reverse proxy (`CF-Connecting-IP`, `X-Forwarded-For`, `X-Real-IP`). Rate limits are per client IP, so set this behind a proxy — but **only** if the server can't be reached except through that proxy, or clients can spoof it. |
| `-allowed-origins` | `QV_ALLOWED_ORIGINS` | *(none)* | Comma-separated extra origins allowed to open WebSockets (the page's own origin is always allowed). |

### Abuse limits

Quick Vote has no accounts, so it protects a public deployment with fixed
limits sized so a group sharing one IP (same Wi-Fi) won't hit them:

- Vote creation: 10 per IP (refilling 1/minute), plus a global cap.
- Other writes (join, suggest, vote, …): 120-request burst per IP, refilling 2/second.
- Participants: 100 per vote.
- Live-update WebSockets: 200 per vote, 50 per IP.
- Request bodies: 64 KiB. HTTP read/write timeouts guard against slow clients.

Responses carry a strict Content-Security-Policy, `X-Frame-Options: DENY`, and
`Referrer-Policy: no-referrer` (room links are the only access control, so
they must not leak via `Referer`).

### Rebuilding the embedded frontend

The Go binary serves the SPA from `webembed/dist` via `//go:embed`. A
placeholder `index.html` is committed so `go build` works from a fresh clone
(the API works; the page just says the UI wasn't built). To
embed the real UI when building outside Docker:

```sh
cd web && npm run build
cp -r dist/. ../webembed/dist/
go build ./cmd/quickvote
```

The Docker build does this automatically in its multi-stage pipeline.

## Reverse proxy note

Quick Vote uses a WebSocket for live updates at `/api/votes/{slug}/ws`. If you
put it behind a reverse proxy, forward the WebSocket upgrade headers. For nginx:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
}
```

Caddy and Traefik proxy WebSockets correctly with no extra configuration.

Behind any proxy, also set `QV_TRUSTED_IP_HEADER` (e.g. `X-Real-IP` with
`proxy_set_header X-Real-IP $remote_addr;` in nginx, or `CF-Connecting-IP` for
a Cloudflare Tunnel) so rate limits apply per client rather than to the proxy.

## License

[MIT](LICENSE).
