# Quick Vote — publish round design

Date: 2026-09-28. Status: approved in conversation, pending written-spec review.

## Purpose

Make Quick Vote fit to show strangers, both as a public demo (vote.aakster.net)
and as a public GitHub project: creators can clean up junk, data is durable and
backed up, the repo tests itself and keeps its dependencies current, and three
small features round out the product.

**Success =** every item below is implemented with tests, the whole branch
passes review, CI is green on GitHub, and the round is deployed and verified
in a real browser.

**Constraints (unchanged from earlier specs):** no accounts; the link is the
only access control; individual ballots are never revealed to anyone but
their owner; phases, settings, and scoring are unchanged.

**Approach:** one branch (`publish-round`), one plan, ordered foundations
first so shared seams (room-state snapshot, store migrations, test clock)
land before the features that build on them. One whole-branch review, one
deploy.

## Part A — Foundations

### A1. Injectable clock (fixes the flaky timer tests)

- New `internal/clock` package: `type Clock interface { Now() time.Time;
  AfterFunc(d time.Duration, f func()) Timer }` with `Timer.Stop() bool`.
  `clock.Real()` wraps `time`; `clock.NewFake(start)` has `Advance(d)` that
  fires due timers synchronously in deadline order.
- `Scheduler` and `Server` take the clock (`Config.Clock`, default real).
  Every `time.Now()` in `internal/server` that affects behavior (deadlines,
  created/joined timestamps, pruning cutoff, last-activity) goes through it.
- The existing wall-clock tests (`TestSuggestTimerAutoAdvance`,
  `TestSuggestTimerHoldsWithTooFewOptions`, `timers_test.go`) are rewritten on
  the fake clock: no `time.Sleep` for timer behavior anywhere in the suite.
- Acceptance: `go test -count=50 ./internal/server/` passes; server test
  package runs faster than before.

### A2. Graceful shutdown + WAL checkpointing

- `main` uses `signal.NotifyContext(SIGINT, SIGTERM)`. On signal:
  `httpSrv.Shutdown` with a 10 s drain, then `Server.Close()` (stops the
  scheduler, closes all WebSockets with a normal close frame, stops the
  prune/checkpoint loops), then `Store.Close()`.
- `Store.Checkpoint()` runs `PRAGMA wal_checkpoint(TRUNCATE)`. Called hourly
  by a background loop and by `Store.Close()` before closing the DB.
- Acceptance: after a clean container stop, `quickvote.db` holds the data and
  `-wal` is empty or absent (store test: write, Close, reopen the file with
  WAL disabled/read-only, rows present). Tests cover Checkpoint and Close.

### A3. Last-activity retention

- Migration: `votes.last_activity INTEGER`, backfilled
  `UPDATE votes SET last_activity = created_at WHERE last_activity IS NULL`.
  New votes set it to created time.
- `Server.changed(slug)` (the single post-mutation hook) bumps
  `last_activity` to clock-now before broadcasting. Every mutating handler
  already calls `changed`; the plan verifies each one does.
- Prune deletes votes with `last_activity < now - retention` (replacing the
  `created_at` rule). `QV_RETENTION_DAYS` semantics become "days since last
  activity"; README updated.
- Acceptance: a vote created 100 days ago but touched 1 day ago survives
  pruning; an untouched one is removed.

### A4. Broadcast fan-out loads once

- `broadcast(slug)` loads vote, participants, options, and ballots once, then
  for each connection resolves the requester from the in-memory participant
  list (by token, constant-time compare) and calls `BuildRoomState`.
  `pushSnapshot` for a single connection (initial push) uses the same path.
- Acceptance: a store-level query counter (test hook) shows a broadcast to N
  connections issues a constant number of queries independent of N; existing
  WS tests pass.

### A5. WebSocket auth by first message

- The `?token=` query parameter is removed. After upgrade, the server waits up
  to 5 s for the first client message: `{"type":"auth","token":"..."}`
  (token may be empty/absent → spectator). Invalid JSON, wrong type, or
  timeout → close with policy-violation code. Only after auth does the
  connection join the hub and receive its first snapshot. Subsequent client
  messages are still ignored.
- The client (`web/src/ws.ts`) sends the auth message on `open`.
- Old tabs that don't send auth are closed after 5 s and reconnect as the new
  client after a refresh; acceptable.
- Acceptance: tests for authed connect, spectator connect, no-message timeout
  (on the fake clock or a short injected timeout), garbage first message.

## Part B — Creator powers

All creator endpoints require a valid session (`Authorization: Bearer`)
belonging to the creator participant **and** `X-Creator-Token` (constant-time
compare), matching the existing `advance` rules. All run under the per-slug
lock and call `changed(slug)`.

### B1. Remove participant

- `DELETE /api/votes/{slug}/participants/{id}` → 200 with room state.
- 400 if `id` is the creator; 404 if no such participant in this vote.
- Removal is a **soft delete** (FKs from options/ballots to participants stay
  valid). Migration: `participants.removed_at INTEGER NULL`. In one store
  transaction: set `removed_at`, replace the participant's `token` with a
  fresh random value never returned to anyone (so the old token stops
  authenticating), clear `wants_revote` / `done_suggesting`, delete their
  ballot.
- Their suggestions: **suggest phase** → deleted (hard). **Voting / results
  phase** → kept, because other participants may have spent credits on them
  and deleting would silently rewrite others' ballots and budgets. The client
  attributes such options to "removed participant". Stored results are never
  recomputed by a removal.
- Every participant read used for state, counts, and thresholds
  (`Participants`, `ParticipantByToken`, re-vote threshold, all-voted /
  all-done checks, the participant cap, name dedupe) excludes removed rows.
- Their live WebSocket connections are downgraded: on the next broadcast
  their token no longer resolves, so they receive the spectator view.
- If the vote is in the voting phase and `voteAdvanceMode` is `all-voted`,
  re-evaluate auto-advance (the removed person may have been the last
  holdout). Same for suggest-phase `all-done`.
- Client: a session whose token no longer resolves (state `you` is null while
  a stored session exists) shows "You're no longer in this vote" and a fresh
  join form; the stale session is cleared.

### B2. Creator may delete any suggestion

- Existing `DELETE /suggestions/{id}` also succeeds when the caller presents a
  valid creator token (session must still be the creator's). Suggest phase
  only, as today (409 otherwise) — no ballots exist yet, so no ballot
  rewriting is needed.

### B3. Close / reopen

- Migration: `votes.closed_at INTEGER NULL`.
- `POST /api/votes/{slug}/close` and `POST /api/votes/{slug}/reopen` →
  room state. Close is idempotent; reopen of an open vote is a no-op 200.
- While closed, these return 409 "this vote is closed": join, suggestion
  create/delete, done-suggesting, ballot, revote, advance, next (B4), remove
  participant. GET, WS, CSV, and results view keep working.
- Closing clears any armed phase deadline (scheduler + stored deadline).
  Reopening does **not** re-arm a timer; the phase continues manually.
- Room state gains `"closed": bool`.
- Client: "This vote is closed" banner; all inputs disabled; creator sees
  Close / Reopen button (Close available in every phase).
- Recent-votes history entries record `closed` whenever a room state is seen;
  the Home list labels closed votes and sorts them below open ones.

### B4. Start another vote with this group

- Migration: `votes.next_slug TEXT NULL`.
- `POST /api/votes/{slug}/next` `{title, settings?}` → 201
  `{slug, creatorToken, sessionToken, state}` (same shape as create).
  Allowed in any phase while open; 409 if closed or if `next_slug` is already
  set (one successor per vote). Settings default to the source vote's
  normalized settings; a provided settings patch is applied over them and
  validated as in create.
- Creates the new vote and copies every current participant (name,
  is_creator) with **fresh participant IDs and session tokens**; a new creator
  token. Sets the old vote's `next_slug`. Counts against the create rate
  limit.
- Room state of the old vote gains `"next": {"slug", "title"}` for everyone,
  plus, in the requester's personalized `you`, `"nextSessionToken"` (and
  `"nextCreatorToken"` for the creator) — only ever sent to that participant.
- Client: on seeing `you.nextSessionToken`, save the new session under the
  new slug, add it to history, and navigate to `/v/{next}` with a one-time
  notice "Moved to the next vote: *title*". Spectators / removed see a
  "This group moved on → *title*" link instead.

## Part C — Sharing features

### C1. Results export (totals only)

- `GET /api/votes/{slug}/results.csv` — 409 unless results exist.
  `Content-Type: text/csv; charset=utf-8`,
  `Content-Disposition: attachment; filename="<slug>-results.csv"`.
- Columns: `rank,option,score,backers,vetoes,eliminated,winner`. Rows in
  results order; header comment rows are **not** used (plain CSV). No
  per-person data.
- CSV/formula injection: any cell beginning with `=`, `+`, `-`, `@`, tab, or
  CR is prefixed with `'`. Uses `encoding/csv` for quoting.
- Client: "Download CSV" button on the results screen (a plain `<a href>`
  to the endpoint — same-origin, no JS download needed).

### C2. Read-only results link

- Route `/v/{slug}/results`: renders the spectator room state read-only with
  no join gate — results if available, otherwise the current phase summary
  with "Results aren't in yet". Shows "Join this vote" when the vote is open
  and not in results.
- Results screen gets "Copy results link".

### C3. QR code

- "Show QR" toggle beside "Copy share link" renders the join URL as a QR code
  client-side using the `qrcode` npm package (rendered to a data-URL `<img>`;
  CSP already allows `img-src data:`). No external service.

## Part D — Repo, ops, docs

### D1. CI

`.github/workflows/ci.yml` on push and pull_request, three jobs:
`go` (setup-go from `go.mod`, `go vet ./...`, `go test -race ./...`),
`web` (Node 24, `npm ci`, `tsc --noEmit`, `vitest run`, `vite build`),
`docker` (`docker build .`). Actions pinned to major versions.

### D2. Dependabot

`.github/dependabot.yml`, weekly: `gomod` (/), `npm` (/web), `docker` (/),
`github-actions` (/). Minor+patch grouped per ecosystem.

### D3. Frontend dependency upgrade

Vite, Vitest, and `@vitejs/plugin-react` to current majors (plus React types
as needed); `npm audit` reports 0 vulnerabilities including dev deps.

### D4. README

"What is quadratic voting" paragraph; live demo link (vote.aakster.net); CI
badge; 2–3 screenshots in `docs/screenshots/` captured with headless
Chromium against a local instance seeded with sample data (never production
data); API table updated with the new endpoints; retention wording updated.

### D5. Backups (host side, not in the repo)

`/opt/openclaw/scripts/backup-vote.sh` following the existing per-stack
scripts: consistent snapshot via sqlite `.backup` (never a bare file copy —
WAL), scheduled like its siblings.

## API summary (new and changed)

```
DELETE /api/votes/{slug}/participants/{id}   creator; remove + wipe
DELETE /api/votes/{slug}/suggestions/{id}    now also creator
POST   /api/votes/{slug}/close               creator
POST   /api/votes/{slug}/reopen              creator
POST   /api/votes/{slug}/next                creator; {title, settings?}
GET    /api/votes/{slug}/results.csv         public; totals only
GET    /api/votes/{slug}/ws                  auth via first message (no ?token)
```

Room state additions: `closed`, `next {slug,title}` (null when unset),
`you.nextSessionToken`, `you.nextCreatorToken`.

## Testing

- Go: TDD per task; every new endpoint gets happy path, auth failure
  (no session / non-creator / wrong creator token), closed-vote 409, and
  phase rules. Store migration tested against a pre-round schema.
  `go test -race` clean; timer tests on the fake clock.
- Web: Vitest for pure logic (history closed-vote labeling/sorting, next-vote
  session handoff). Typecheck and build.
- End-to-end before deploy: built image in headless Chromium — create, join
  from a second context, live updates, QR renders, close/reopen, remove
  participant, next-vote redirect of the second context, CSV download,
  results link; console free of CSP violations.

## Error handling

Consistent with existing conventions: 400 validation, 401 missing/invalid
session, 403 creator token wrong, 404 unknown vote/participant/option, 409
phase/closed/state conflicts, 429 rate limits, generic 500 without internals.

## Non-goals

Accounts; revealing individual ballots; scoring changes; restarting a vote in
place; hard delete on close; server-side QR generation.
