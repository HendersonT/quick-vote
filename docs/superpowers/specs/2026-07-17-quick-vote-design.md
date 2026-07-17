# Quick Vote — Design Spec

**Date:** 2026-07-17
**Status:** Approved by user

## Purpose

A small, easily self-hostable containerized web app that helps two or more
people decide between suggested games (or anything else) using quadratic
voting. A creator sets up a vote, shares a link, participants suggest options,
then vote quadratically, then see a scored result with veto and re-vote
mechanics.

## Stack

- **Backend:** Go 1.22+, `chi` router, `gorilla/websocket`,
  `modernc.org/sqlite` (pure Go, CGO-free).
- **Frontend:** React 18 + TypeScript + Vite, custom CSS (no UI framework),
  light/dark via `prefers-color-scheme`, mobile-friendly.
- **Packaging:** multi-stage Dockerfile — Node stage builds the SPA, Go stage
  embeds the `dist/` via `embed.FS` and compiles a static binary; final image
  is minimal (alpine or distroless). One port (default 8080), one volume
  (`/data`) holding the SQLite file. A `docker-compose.yml` is provided for
  one-command hosting.
- **Persistence:** SQLite file at `/data/quickvote.db`.

## Domain model

### Vote (a topic)

Created from the home page. Fields / creation-time settings:

| Setting | Default | Notes |
|---|---|---|
| Title | required | e.g. "Friday game night" |
| Max suggestions per user | 3 | 1–20 |
| Credits per option | 3 | Budget multiplier; total budget = this × option count |
| Suggestion-phase advance rule | manual | `manual` (creator advances) or `count:N` (auto-advance once N participants have each submitted ≥1 suggestion) |
| Voting-phase advance rule | all-voted | `manual` or `all-voted` (auto-advance once every participant has submitted a ballot) |
| Survival threshold | 1 | Minimum total score an option needs to avoid elimination. Default 1 ⇒ a total score of zero is a veto. Creator may raise it. |
| Tiebreaker | most-backers | `most-backers` (most distinct voters, then random), `random`, or `creator` (creator picks among tied) |
| Re-vote threshold | 33% | Percent of participants needed to trigger a re-vote |
| Suggestion timer | off | Optional duration; expiry auto-advances phase |
| Voting timer | off | Optional duration; expiry auto-advances phase |

Each vote gets a short random URL slug (e.g. `/v/x7Kp2q`). The creator holds a
separate creator token gating creator-only controls.

Random values (slugs, tokens, random tiebreaks) use crypto/rand on the server.

### Participant

- Joins via the shared link by entering a display name.
- Server issues a session token; client stores it in `localStorage` keyed by
  vote slug, so reopening the link resumes the same identity.
- The creator is a participant too (joins with a name like everyone else; the
  create flow takes them straight to the room with both tokens).
- Duplicate display names get a numeric suffix (`Sam`, `Sam (2)`).

### Option (suggestion)

- Text title, attributed to the suggesting participant.
- A participant may delete their own suggestions during the suggestion phase.
- Cap enforced server-side: at most *max suggestions per user* each.
- Duplicate titles (case-insensitive) are rejected with a friendly error.

### Ballot

- Per participant: a map option → integer vote count `v ≥ 0`.
- Quadratic cost: `v` votes on one option cost `v²` credits.
- Total spent = Σ v² over options; must be ≤ budget
  (= credits-per-option × option count).
- Client shows a live budget meter with +/- steppers per option; server
  re-validates on submit and rejects overruns.
- A participant may resubmit (overwrite) their ballot until the voting phase
  ends.

## Lifecycle

Phases: `suggesting → voting → results`.

Advancement, in any combination:
1. The configured auto-rule fires (count-of-suggesters / all-voted).
2. The creator advances manually at any time (always allowed, regardless of
   the configured rule).
3. A phase timer expires (server-side scheduler; the deadline timestamp is in
   the room state so clients render a live countdown).

Edge rules:
- Advancing to voting with fewer than 2 options is blocked (creator gets an
  explanatory error; timers with <2 options hold the phase and notify).
- Participants may join at any phase; late joiners in `voting` can vote,
  late joiners in `results` only spectate and count toward re-vote
  denominators only if they joined before results were computed. (Simplify:
  re-vote denominator = participants at the moment of counting.)

### Scoring (at entry to results)

1. Score of each option = sum of all participants' votes on it.
2. Options with score < survival threshold are **eliminated** (marked as
   vetoed in the UI).
3. Highest-scoring surviving option wins; ties resolved by the configured
   tiebreaker. `creator` tiebreak: results show "tied — waiting for creator",
   creator picks among tied options.
4. If **all** options are eliminated: no winner; the results screen says so
   and suggests a re-vote.

### Re-vote

- On the results screen every participant has a "call for re-vote" toggle.
- When calls ≥ ceil(re-vote-threshold% × current participant count), all
  ballots and re-vote calls are cleared and the vote returns to `voting`
  (same options, same budget; the voting timer restarts if configured).
- Repeatable any number of times.

## API

REST (JSON) for mutations, WebSocket for state push.

```
POST   /api/votes                      create vote → {slug, creatorToken, sessionToken}
GET    /api/votes/{slug}               fetch room state (also sent over WS)
POST   /api/votes/{slug}/join          {name} → {sessionToken}
POST   /api/votes/{slug}/suggestions   {title}
DELETE /api/votes/{slug}/suggestions/{id}
PUT    /api/votes/{slug}/ballot        {votes: {optionId: n, ...}}
POST   /api/votes/{slug}/advance       creator only; body may carry {winnerOptionId} for creator tiebreak
POST   /api/votes/{slug}/revote        toggle this participant's re-vote call
GET    /api/votes/{slug}/ws            WebSocket; pushes full room-state snapshot on every change
```

- Auth: session token via `Authorization: Bearer`; creator token via
  `X-Creator-Token` where required.
- Room state is one JSON snapshot (vote settings, phase, deadline, options,
  participants with submitted/voted flags, ballots only revealed in results,
  scores/winner in results). Clients render purely from this snapshot;
  WS reconnect just refetches it — no incremental sync to desync.
- Phase-illegal actions → 409 with a human-readable message. Unknown slug →
  404 page.

## UI

Three screens:

1. **Home / create** — title field plus defaults-filled settings; less-common
   settings (timers, survival threshold, tiebreaker, re-vote threshold)
   behind an "Advanced" fold. Submitting creates the vote, prompts for the
   creator's display name, and lands in the room with a prominent
   copy-share-link button.
2. **Vote room** (`/v/{slug}`) — name-entry gate for new joiners, then a
   phase-appropriate body: suggestion list + add box (suggesting), ballot with
   steppers + budget meter (voting), results view (results). Persistent
   elements: title, phase indicator, countdown timer when set, participant
   sidebar with per-phase status (suggested / voted / called re-vote),
   creator-only "advance phase" button.
3. **Results** — score bars per option, vetoed options greyed with a veto
   marker, winner banner, tiebreak explanation when applied, re-vote button
   with live call count (e.g. "2 of 3 needed").

## Testing

- **Go unit tests:** scoring engine (quadratic cost + budget validation,
  survival threshold/veto, every tiebreaker, all-eliminated case, re-vote
  threshold math), slug/token generation.
- **Go httptest API tests:** full lifecycle (create → join×N → suggest →
  advance → ballots → results → re-vote), auth failures, phase-illegal
  actions, suggestion caps, budget overrun rejection.
- **Frontend:** Vitest unit tests for the budget calculator; the rest of the
  UI is kept thin enough to verify by exercising the app.

## Error handling

- Server validates everything; client-side checks are UX only.
- WS auto-reconnects with exponential backoff and refetches state on
  reconnect.
- SQLite writes serialized through a single writer (Go mutex or `_txlock`);
  WAL mode on.

## Out of scope (YAGNI)

- Accounts/passwords, email, federation.
- Editing vote settings after creation.
- Multiple concurrent polls per room, vote history/archive UI (old votes
  remain reachable by slug until pruned; pruning itself is out of scope).
- Horizontal scaling (single instance by design).
