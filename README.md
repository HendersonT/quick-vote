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
2. **Voting** — everyone gets a shared budget of `credits per option × number of
   options`. Casting `v` votes on one option costs `v²` credits, so spreading
   support is cheap and piling onto one option is expensive. You can resubmit
   your ballot until the phase ends.
3. **Results** — each option's score is the sum of votes cast on it. Options
   below the survival threshold are vetoed; the highest surviving score wins,
   with ties resolved by the configured tiebreaker. Anyone can call for a
   re-vote; once enough people do, ballots clear and the room returns to voting.

Phase changes and every other update are pushed live over a WebSocket, so all
open browsers stay in sync.

## Settings glossary

Set at creation time (the last few live behind an "Advanced" fold):

| Setting | Default | Meaning |
|---|---|---|
| **Title** | required | Name of the vote, e.g. "Friday game night". |
| **Max suggestions per user** | 3 | Cap on how many options each participant may add (1–20). |
| **Credits per option** | 3 | Budget multiplier. Total budget = this × number of options. Higher = more expressive ballots. |
| **Suggestion-phase advance rule** | manual | `manual` (creator advances) or `count:N` (auto-advance once N distinct participants have each submitted at least one suggestion). |
| **Voting-phase advance rule** | all-voted | `manual` or `all-voted` (auto-advance once every current participant has submitted a ballot). |
| **Survival threshold** | 1 | Minimum total score an option needs to avoid being vetoed. `0` disables vetoes; the default `1` means a total score of zero is eliminated. Raise it to require broader support. |
| **Tiebreaker** | most-backers | How a tie for the top score is broken: `most-backers` (most distinct voters, then random), `random`, or `creator` (results pause and the creator picks among the tied options). |
| **Re-vote threshold** | 33% | Percentage of current participants whose re-vote calls are needed to send the room back to voting (rounded up, minimum 1). |
| **Suggestion timer** | off | Optional duration; when it expires the suggestion phase auto-advances (held if fewer than 2 options exist). |
| **Voting timer** | off | Optional duration; when it expires the voting phase auto-advances and results are scored. |

The creator is a participant like everyone else and holds a separate creator
token (kept in the browser) that gates the "advance phase" and creator-tiebreak
controls. Settings cannot be changed after a vote is created.

## Development

Backend (Go 1.23+):

```sh
go run ./cmd/quickvote -addr :8080 -db ./quickvote.db
```

Frontend (Node 22), with a dev server that proxies `/api` (including the
WebSocket) to the Go backend on `:8080`:

```sh
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

### Rebuilding the embedded frontend

The Go binary serves the SPA from `webembed/dist` via `//go:embed`. A minimal
placeholder `index.html` is committed so `go build` works from a fresh clone. To
embed the real UI when building outside Docker:

```sh
cd web && npm run build
cp -r dist ../webembed/dist
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

## License

Provided as-is for self-hosting.
