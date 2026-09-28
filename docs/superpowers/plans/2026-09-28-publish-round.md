# Publish Round Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden Quick Vote for public use (creator moderation, close/reopen, durable data, last-activity retention, efficient/secure WebSockets), add three sharing features (next vote with the same group, results export/link, QR), and give the repo CI, Dependabot, current tooling, and a real README.

**Architecture:** Go server (`chi`, `gorilla/websocket`, `modernc.org/sqlite`) with an embedded React/Vite SPA. Foundations land first (test clock → store migrations → activity/shutdown → broadcast refactor → WS auth) so the creator-power and sharing tasks build on stable seams. Every mutation still flows through per-slug locks and the single `Server.changed(slug)` hook.

**Tech Stack:** Go 1.23+ (image builds with 1.27), SQLite (modernc, WAL), React 18 + TypeScript, Vite 8 / Vitest 5 (after Task 11), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-28-publish-round-design.md` — read it alongside this plan; section IDs (A1…D5) are referenced per task.

## Global Constraints

- Branch: `publish-round`. Commit per task; messages `type: summary` ending with the two trailer lines:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01QdFq7Vk7R975qFWQTUXzoS`.
- Module path `github.com/HendersonT/quick-vote`. Keep `go 1.23` in `go.mod`; add **no new Go module dependencies**.
- Only new web runtime dependency: `qrcode` (+ `@types/qrcode` dev).
- No `time.Sleep` to wait for timer/phase behavior in tests — use the fake clock (Task 1). Sleeps are allowed only for real-network WS plumbing where unavoidable, ≤100 ms.
- HTTP error conventions: 400 validation, 401 missing/invalid session, 403 wrong creator token, 404 unknown vote/participant/option, 409 phase/closed/state conflict, 429 rate limit, generic `"internal error"` 500.
- Individual ballots are never exposed except to their owner (`you.ballot`). CSV/results view: totals only.
- Creator-only endpoints require **both** a creator session (`Authorization: Bearer`, participant `isCreator`) and `X-Creator-Token` (constant-time compare).
- Every mutating handler: per-slug lock → load vote → closed check (Task 8 helper) → auth → work → `s.changed(slug)` → respond.
- Comment style: doc comments explain *why*, matching surrounding code. `gofmt` clean; `go vet ./...` clean; `go test -race ./...` passes.
- Frontend: `npm run build` (tsc + vite) and `npm test` pass; no `dangerouslySetInnerHTML`; nothing that violates the CSP in `internal/server/limits.go` (no inline scripts, no external origins).
- Never use production data (`/opt/services/vote/data`) in tests or screenshots.

## Review Focus

1. A participant removed while their browser has a live WebSocket must drop to the spectator view and see the "no longer in this vote" notice — never a silently read-only room. (Test: Task 6, `TestRemovedParticipantWSDowngradesToSpectator`.)
2. Returning to an old vote after the group moved on must not redirect-loop; auto-move happens once per old slug. (Test: Task 12, `shouldAutoMove` Vitest cases.)
3. Suggestion titles containing `=`,`+`,`-`,`@`, commas, quotes, or newlines must come out of the CSV as inert, correctly-quoted text. (Test: Task 10, `TestResultsCSVNeutralizesFormulas`.)
4. A phase timer armed before the vote is closed must not advance a closed vote when it fires. (Test: Task 8, `TestClosedVoteTimerDoesNotAdvance`.)
5. "Next vote" must carry over only non-removed participants, keep names unique, and respect the 100-participant cap. (Test: Task 9, `TestNextVoteSkipsRemovedParticipants`.)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/clock/clock.go` (new) | `Clock`/`Timer` interfaces, real clock |
| `internal/clock/fake.go` (new) | deterministic fake clock for tests |
| `internal/store/store.go` | schema/migrations, new columns, checkpoint, touch, soft-remove, next-vote tx, query counter |
| `internal/server/server.go` | `Config` gains `Clock`, `WSAuthTimeout`; `Close()`; routes |
| `internal/server/timers.go` | scheduler on the injected clock; timer no-op when closed |
| `internal/server/retention.go` | prune by last activity; background loops (prune daily, checkpoint hourly) |
| `internal/server/ws.go` | first-message auth; broadcast loads once |
| `internal/server/snapshot.go` (new) | `roomData` load-once + per-requester build |
| `internal/server/creator.go` (new) | remove participant, close/reopen, next vote handlers + `requireCreator` |
| `internal/server/export.go` (new) | results CSV |
| `cmd/quickvote/main.go` | signal handling + graceful shutdown |
| `web/src/ws.ts` | send auth message on open |
| `web/src/types.ts`, `web/src/api.ts` | new state fields + API calls |
| `web/src/session.ts` | history `closed`, sort; next-vote handoff helpers |
| `web/src/components/ConfirmButton.tsx` (new) | inline two-step confirm |
| `web/src/components/SettingsFields.tsx` (new) | settings inputs extracted from Home for reuse |
| `web/src/components/NextVoteForm.tsx` (new) | "Start another vote with this group" |
| `web/src/components/QrCode.tsx` (new) | QR toggle |
| `web/src/pages/ResultsView.tsx` (new) | `/v/:slug/results` read-only page |
| `.github/workflows/ci.yml`, `.github/dependabot.yml` (new) | CI + updates |
| `README.md`, `docs/screenshots/*.png` | docs |

---

### Task 1: Injectable clock; timer tests on a fake clock (spec A1)

**Files:**
- Create: `internal/clock/clock.go`, `internal/clock/fake.go`, `internal/clock/fake_test.go`
- Modify: `internal/server/server.go` (Config.Clock, field `clock`), `internal/server/timers.go`, `internal/server/advance.go` (3× `time.Now()`), `internal/server/handlers.go` (every `time.Now()`), `internal/server/retention.go` (`time.Now()`)
- Modify tests: `internal/server/timers_test.go`, `internal/server/lifecycle_test.go` (`TestSuggestTimerAutoAdvance`, `TestSuggestTimerHoldsWithTooFewOptions`), add helper `newFakeClockServer` in `internal/server/handlers_test.go`

**Interfaces:**
- Produces: package `clock` — `type Clock interface { Now() time.Time; AfterFunc(d time.Duration, f func()) Timer }`, `type Timer interface { Stop() bool }`, `func Real() Clock`, `type Fake struct`, `func NewFake(start time.Time) *Fake`, `(*Fake).Advance(d time.Duration)`, `(*Fake).Now()`, `(*Fake).AfterFunc(...)`.
- Produces: `server.Config.Clock clock.Clock` (nil → `clock.Real()`); `(*Server).now() time.Time`; `NewScheduler(c clock.Clock, fire func(string)) *Scheduler`.
- Produces test helper (package `server_test`): `func newFakeClockServer(t *testing.T) (*server.Server, *clock.Fake)` backed by a temp store.

- [ ] **Step 1: Write failing fake-clock tests** — `internal/clock/fake_test.go`:

```go
package clock

import (
	"testing"
	"time"
)

func TestFakeAdvanceFiresDueTimersInOrder(t *testing.T) {
	c := NewFake(time.Unix(1000, 0))
	var got []string
	c.AfterFunc(2*time.Second, func() { got = append(got, "b") })
	c.AfterFunc(1*time.Second, func() { got = append(got, "a") })
	late := c.AfterFunc(5*time.Second, func() { got = append(got, "late") })

	c.Advance(3 * time.Second)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fired %v, want [a b]", got)
	}
	if !late.Stop() {
		t.Fatal("Stop on a pending timer should report true")
	}
	c.Advance(10 * time.Second)
	if len(got) != 2 {
		t.Fatalf("stopped timer fired: %v", got)
	}
	if want := time.Unix(1013, 0); !c.Now().Equal(want) {
		t.Fatalf("Now = %v, want %v", c.Now(), want)
	}
}

func TestFakeZeroDelayFiresOnNextAdvance(t *testing.T) {
	c := NewFake(time.Unix(0, 0))
	fired := false
	c.AfterFunc(0, func() { fired = true })
	c.Advance(0)
	if !fired {
		t.Fatal("zero-delay timer should fire on Advance(0)")
	}
}

func TestFakeCallbackMaySchedule(t *testing.T) {
	c := NewFake(time.Unix(0, 0))
	n := 0
	c.AfterFunc(time.Second, func() {
		n++
		c.AfterFunc(time.Second, func() { n++ })
	})
	c.Advance(2 * time.Second)
	if n != 2 {
		t.Fatalf("n = %d, want 2 (timer scheduled inside a callback and due within the advance window fires)", n)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/clock/` — Expected: FAIL (package has no non-test files / undefined `NewFake`).

- [ ] **Step 3: Implement** `internal/clock/clock.go`:

```go
// Package clock abstracts time so timer-driven behavior (phase deadlines,
// pruning, last-activity stamps) can be tested deterministically.
package clock

import "time"

// Clock is the subset of the time package the server depends on.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a cancellable pending callback.
type Timer interface {
	// Stop prevents the callback from running, reporting whether it was
	// still pending.
	Stop() bool
}

type realClock struct{}

// Real returns a Clock backed by the time package.
func Real() Clock { return realClock{} }

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
```

`internal/clock/fake.go`:

```go
package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake is a manually advanced Clock. Timers fire synchronously, in deadline
// order, on the goroutine calling Advance — never on their own — so tests
// observe phase transitions without sleeping.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	seq    int
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *Fake
	at      time.Time
	seq     int
	f       func()
	stopped bool
	fired   bool
}

// NewFake returns a Fake clock reading start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the fake current time.
func (c *Fake) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules f to run once the clock has been advanced past d.
func (c *Fake) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 {
		d = 0
	}
	c.seq++
	t := &fakeTimer{c: c, at: c.now.Add(d), seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward by d, firing every timer due at or before
// the new time (including timers scheduled by callbacks during the advance)
// in (deadline, creation) order. Callbacks run without the clock's lock held.
func (c *Fake) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var due *fakeTimer
		sort.SliceStable(c.timers, func(i, j int) bool {
			if !c.timers[i].at.Equal(c.timers[j].at) {
				return c.timers[i].at.Before(c.timers[j].at)
			}
			return c.timers[i].seq < c.timers[j].seq
		})
		for i, t := range c.timers {
			if t.stopped || t.fired {
				continue
			}
			if !t.at.After(target) {
				due = t
				c.timers = append(c.timers[:i], c.timers[i+1:]...)
				break
			}
		}
		if due == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		if due.at.After(c.now) {
			c.now = due.at
		}
		due.fired = true
		c.mu.Unlock()
		due.f()
	}
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}
```

- [ ] **Step 4: Run** `go test ./internal/clock/` — Expected: PASS.

- [ ] **Step 5: Wire the clock into the server.** In `server.go` add to `Config`:

```go
	// Clock drives deadlines, timestamps and pruning. nil means real time;
	// tests inject clock.NewFake.
	Clock clock.Clock
```

Add field `clock clock.Clock` to `Server`; in `NewWithConfig` set `if cfg.Clock == nil { cfg.Clock = clock.Real() }`, `s.clock = cfg.Clock`, and construct the scheduler as `NewScheduler(s.clock, func(slug string) { s.timerFired(slug) })`. Add:

```go
// now is the server's notion of the current time (injectable for tests).
func (s *Server) now() time.Time { return s.clock.Now() }
```

In `timers.go`: `Scheduler` gets `clock clock.Clock`, `timers map[string]clock.Timer`; `NewScheduler(c clock.Clock, fire func(slug string))`; in `Set` replace `time.Until(at)` with `at.Sub(sc.clock.Now())` and `time.AfterFunc` with `sc.clock.AfterFunc`. Replace **every** `time.Now()` in `advance.go` (two deadline computations; leave `newSeededRand`'s fallback seed alone), `handlers.go` (create, join, suggestion `CreatedAt`, revote deadline), and `retention.go` (`PruneExpired` cutoff) with `s.now()`. Verify with `grep -n "time.Now()" internal/server/*.go` — only `newSeededRand` and `ws.go` deadline plumbing (`SetWriteDeadline`/`SetReadDeadline`, which are real network I/O) may remain.

- [ ] **Step 6: Add the fake-clock test helper** to `internal/server/handlers_test.go`:

```go
// newFakeClockServer returns a server whose deadlines and timestamps run on
// a manually advanced clock, so timer behavior is tested without sleeping.
func newFakeClockServer(t *testing.T) (*server.Server, *clock.Fake) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	return server.NewWithConfig(st, nil, server.Config{Clock: fc}), fc
}
```

- [ ] **Step 7: Rewrite the wall-clock tests.** In `lifecycle_test.go`, replace the two timer tests:

```go
func TestSuggestTimerAutoAdvance(t *testing.T) {
	s, fc := newFakeClockServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 60})
	suggest(t, s, slug, aliceTok, "Catan")
	suggest(t, s, slug, aliceTok, "Wingspan")

	fc.Advance(59 * time.Second)
	if st := getState(t, s, slug, aliceTok); st["phase"] != "suggesting" {
		t.Fatalf("phase = %v before deadline, want suggesting", st["phase"])
	}
	fc.Advance(time.Second)
	if st := getState(t, s, slug, aliceTok); st["phase"] != "voting" {
		t.Fatalf("phase = %v at deadline, want voting", st["phase"])
	}
}

func TestSuggestTimerHoldsWithTooFewOptions(t *testing.T) {
	s, fc := newFakeClockServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 60})
	suggest(t, s, slug, aliceTok, "Only")

	fc.Advance(61 * time.Second)
	st := getState(t, s, slug, aliceTok)
	if st["phase"] != "suggesting" {
		t.Fatalf("phase = %v, want suggesting held with <2 options", st["phase"])
	}
	if st["phaseDeadline"] != nil {
		t.Fatalf("phaseDeadline = %v, want nil (dropped on timer expiry)", st["phaseDeadline"])
	}
}
```

In `timers_test.go`, build both servers with one shared `clock.NewFake` via `server.NewWithConfig(st, nil, server.Config{Clock: fc})`; persist the past-due deadline as `fc.Now().Add(-time.Minute).Unix()`; after `s2.RearmTimers()` call `fc.Advance(0)` and assert the phase is `results` immediately (delete the polling loop and `time.Sleep`).

- [ ] **Step 8: Run** `go test -race ./...` then `go test -count=30 ./internal/server/` — Expected: all PASS; `grep -n "time.Sleep" internal/server/lifecycle_test.go internal/server/timers_test.go` returns nothing.

- [ ] **Step 9: Commit** — `git add internal/clock internal/server && git commit -m "test: injectable clock; timer tests run on a fake clock"` (+ trailers).

---

### Task 2: Store — migrations, checkpoint, touch, soft-remove columns, query counter (spec A2, A3, B1, B3, B4 data)

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces on `VoteRow`: `LastActivity int64`, `ClosedAt *int64`, `NextSlug *string`, `NextCreatorToken *string`. `GetVote`/`CreateVote`/`UpdateVote` read/write them (CreateVote sets `last_activity = CreatedAt` when `LastActivity == 0`).
- Produces on `ParticipantRow`: `NextToken *string` (read by `Participants`/`ParticipantByToken`).
- Produces methods:
  - `func (s *Store) TouchVote(slug string, at int64) error` — sets `last_activity`.
  - `func (s *Store) Checkpoint() error` — `PRAGMA wal_checkpoint(TRUNCATE)`.
  - `func (s *Store) DeleteVotesInactiveSince(cutoff int64) ([]string, error)` — replaces `DeleteVotesCreatedBefore` (delete the old one; update its only caller in `retention.go`).
  - `func (s *Store) QueryCount() int64` — number of read queries issued by `GetVote`, `Participants`, `ParticipantByToken`, `Options`, `Ballots` (atomic counter; test/diagnostic only).
  - `Close()` now calls `Checkpoint()` (ignoring its error) before `db.Close()`.
- `Participants` and `ParticipantByToken` exclude rows with `removed_at IS NOT NULL`.

- [ ] **Step 1: Write failing tests** in `internal/store/store_test.go` (reuse the file's existing `openTestStore`-style helper; if none, open `filepath.Join(t.TempDir(),"s.db")`):

```go
func TestMigrationAddsPublishRoundColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// Pre-round schema: the July tables without the new columns.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE votes (slug TEXT PRIMARY KEY, title TEXT NOT NULL, phase TEXT NOT NULL DEFAULT 'suggesting',
  settings TEXT NOT NULL, creator_token TEXT NOT NULL, phase_deadline INTEGER, results TEXT,
  created_at INTEGER NOT NULL, active_options TEXT);
CREATE TABLE participants (id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  name TEXT NOT NULL, token TEXT NOT NULL UNIQUE, is_creator INTEGER NOT NULL DEFAULT 0,
  wants_revote INTEGER NOT NULL DEFAULT 0, joined_at INTEGER NOT NULL, done_suggesting INTEGER NOT NULL DEFAULT 0);
INSERT INTO votes (slug,title,settings,creator_token,created_at) VALUES ('old','T','{}','ct',500);
INSERT INTO participants (id,vote_slug,name,token,joined_at) VALUES ('p1','old','A','tok',500);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	defer st.Close()
	v, err := st.GetVote("old")
	if err != nil {
		t.Fatal(err)
	}
	if v.LastActivity != 500 {
		t.Fatalf("last_activity backfill = %d, want created_at 500", v.LastActivity)
	}
	if v.ClosedAt != nil || v.NextSlug != nil || v.NextCreatorToken != nil {
		t.Fatalf("new nullable columns should be nil: %+v", v)
	}
	ps, err := st.Participants("old")
	if err != nil || len(ps) != 1 || ps[0].NextToken != nil {
		t.Fatalf("participants after migration: %+v err=%v", ps, err)
	}
}

func TestTouchAndPruneByLastActivity(t *testing.T) {
	st := newStore(t)
	mk := func(slug string, created int64) {
		if err := st.CreateVote(VoteRow{Slug: slug, Title: slug, Phase: "suggesting", Settings: "{}", CreatorToken: "c" + slug, CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	mk("stale", 100)
	mk("busy", 100)
	if err := st.TouchVote("busy", 900); err != nil {
		t.Fatal(err)
	}
	gone, err := st.DeleteVotesInactiveSince(500)
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0] != "stale" {
		t.Fatalf("pruned %v, want [stale]", gone)
	}
	if _, err := st.GetVote("busy"); err != nil {
		t.Fatalf("recently active vote was pruned: %v", err)
	}
}

func TestCloseCheckpointsWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateVote(VoteRow{Slug: "w", Title: "W", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("-wal is %d bytes after Close, want empty/absent", fi.Size())
	}
	// The main file alone must hold the data.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM votes`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows in main file = %d err=%v, want 1", n, err)
	}
}

func TestQueryCountCountsReads(t *testing.T) {
	st := newStore(t)
	before := st.QueryCount()
	_, _ = st.GetVote("nope")
	_, _ = st.Participants("nope")
	if got := st.QueryCount() - before; got != 2 {
		t.Fatalf("QueryCount delta = %d, want 2", got)
	}
}
```

(`newStore(t)` = the file's existing open helper; add it if absent: open temp DB, `t.Cleanup(st.Close)`.)

- [ ] **Step 2: Run** `go test ./internal/store/` — Expected: FAIL (unknown fields/methods).

- [ ] **Step 3: Implement.**
  - Add columns to the base `schema` string (`votes`: `last_activity INTEGER, closed_at INTEGER, next_slug TEXT, next_creator_token TEXT`; `participants`: `removed_at INTEGER, next_token TEXT`) **and** to `migrationColumns` (`ALTER TABLE ... ADD COLUMN ...` for each of the six).
  - After `migrate`, run the backfill: `UPDATE votes SET last_activity = created_at WHERE last_activity IS NULL`.
  - Extend `VoteRow`/`ParticipantRow` and every SELECT/INSERT/UPDATE that lists their columns. `CreateVote`: `if v.LastActivity == 0 { v.LastActivity = v.CreatedAt }`.
  - `Participants` / `ParticipantByToken`: add `AND removed_at IS NULL`.
  - Add `queries atomic.Int64` to `Store`; `s.queries.Add(1)` at the top of the five read methods; `QueryCount()` returns `s.queries.Load()`.
  - `TouchVote`, `Checkpoint`, `DeleteVotesInactiveSince` (copy of the old prune with `last_activity < ?` in both the SELECT and the sub-select), `Close` checkpointing first. Doc-comment each ("why": WAL never auto-checkpoints below 1000 pages, so a small DB keeps all data in `-wal` until checkpointed).
  - Update `retention.go`'s `PruneExpired` to call `DeleteVotesInactiveSince` (the server-level semantic change and its test land in Task 3).

- [ ] **Step 4: Run** `go test -race ./...` — Expected: PASS (existing `TestPruneExpired` in `internal/server/limits_test.go` still passes: a fresh vote's last activity equals its creation).

- [ ] **Step 5: Commit** — `feat(store): activity/close/next/removal columns, WAL checkpoint, query counter`.

---

### Task 3: Last-activity bump, prune semantics, graceful shutdown, hourly checkpoint (spec A2, A3)

**Files:**
- Modify: `internal/server/server.go` (`changed`, `Close`), `internal/server/retention.go`, `cmd/quickvote/main.go`, `README.md` (retention wording)
- Test: `internal/server/limits_test.go` (extend prune tests), new `internal/server/shutdown_test.go`

**Interfaces:**
- Consumes: `Store.TouchVote`, `Store.Checkpoint`, `Store.DeleteVotesInactiveSince`, `Server.now()`.
- Produces: `func (s *Server) Close()` — idempotent; stops background loops, clears all scheduler timers, closes every hub connection with a normal close frame. Does **not** close the store (main owns it).
- Produces: `func (s *Server) StartBackground(retention time.Duration)` replacing `StartPruning`: prunes at start then daily (skip when retention ≤ 0), checkpoints hourly; both loops exit on `Close()`.

- [ ] **Step 1: Failing tests.** In `limits_test.go` replace `TestPruneExpired` with:

```go
func TestPruneUsesLastActivity(t *testing.T) {
	s, fc := newFakeClockServer(t)
	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": "Busy", "creatorName": "A"}, "")
	busy, busyTok := out["slug"].(string), out["sessionToken"].(string)
	_, out = doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": "Idle", "creatorName": "B"}, "")
	idle := out["slug"].(string)

	fc.Advance(100 * 24 * time.Hour)
	suggest(t, s, busy, busyTok, "fresh activity") // bumps last_activity via changed()

	n, err := s.PruneExpired(90 * 24 * time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("pruned n=%d err=%v, want 1", n, err)
	}
	if rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+idle, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("idle vote survived: %d", rec.Code)
	}
	if rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+busy, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("active vote pruned: %d", rec.Code)
	}
}
```

`shutdown_test.go` (package `server_test`):

```go
func TestServerCloseClosesWebSockets(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, tok, _ := createVote(t, s, nil)
	conn := dialWS(t, ts, slug, tok)
	readSnapshot(t, conn)

	s.Close()
	s.Close() // idempotent

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		t.Fatalf("expected a normal close frame, got %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/server/ -run 'Prune|ServerClose'` — Expected: FAIL.

- [ ] **Step 3: Implement.**
  - `changed(slug)`: first `_ = s.store.TouchVote(slug, s.now().Unix())`, then the existing hook call. (Every mutating handler and `timerFired` already call `changed`; verify with `grep -n "s.changed(" internal/server/*.go` that create-vote does too — it currently does **not**: `handleCreateVote` sets last_activity at creation via `CreateVote`, which is sufficient.)
  - `PruneExpired`: cutoff `s.now().Add(-retention).Unix()` against `DeleteVotesInactiveSince`.
  - `Server` gains `stop chan struct{}`, `closeOnce sync.Once`. `StartBackground` launches one goroutine using `time.NewTicker` (real time is fine here — the daily/hourly cadence isn't under test; `PruneExpired` itself is). `Close()`: `closeOnce.Do` → close `stop`, `s.scheduler.StopAll()` (new `Scheduler` method: stop and delete every timer), then for every connection in the hub send a close frame via `c.close()` after enqueuing: implement `hub.all() []*wsConn` and, in `wsWritePump`'s `<-c.done` case, write `websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server shutting down")` (replacing the empty close payload).
  - `main.go`:

```go
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv.StartBackground(time.Duration(*retentionDays) * 24 * time.Hour)
	// ... httpSrv as before ...
	go func() {
		log.Printf("quickvote: listening on %s, db at %s", *addr, *dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("quickvote: server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("quickvote: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("quickvote: http shutdown: %v", err)
	}
	srv.Close()
	if err := st.Close(); err != nil {
		log.Printf("quickvote: close store: %v", err)
	}
```

  Remove the earlier `defer st.Close()` (it would double-close). Note in a comment that `http.Server.Shutdown` does not wait for hijacked WebSocket connections — `srv.Close()` handles those.
  - README: retention row/paragraph → "deleted after 90 days without activity".

- [ ] **Step 4: Run** `go test -race ./...` — PASS. Manual check: `go build -o /tmp/qv ./cmd/quickvote && /tmp/qv -db /tmp/qvtest/q.db & sleep 1; curl -s -XPOST localhost:8080/api/votes -d '{"title":"t","creatorName":"a"}' >/dev/null; kill -TERM %1; wait; ls -la /tmp/qvtest` — Expected: log line "shutting down", `q.db-wal` absent or 0 bytes. (Use a free port via `-addr` if 8080 is taken — it is on this host: use `-addr 127.0.0.1:18080`.)

- [ ] **Step 5: Commit** — `feat: last-activity retention, graceful shutdown, periodic WAL checkpoint`.

---

### Task 4: Broadcast loads room data once (spec A4)

**Files:**
- Create: `internal/server/snapshot.go`
- Modify: `internal/server/ws.go` (`pushSnapshot`, `broadcast`, remove `buildSnapshotJSON`), `internal/server/handlers.go` (`handleGetVote`, `writeState` use the same loader)
- Test: `internal/server/snapshot_test.go` (package `server`, internal — needs `store` access)

**Interfaces:**
- Produces:

```go
// roomData is everything BuildRoomState needs for one vote, loaded once.
type roomData struct {
	vote    store.VoteRow
	parts   []store.ParticipantRow
	opts    []store.OptionRow
	ballots map[string]map[string]int
}

func (s *Server) loadRoom(slug string) (roomData, error)

// requester resolves token against the already-loaded participants with a
// constant-time compare; nil for "" or no match (spectator).
func (d roomData) requester(token string) *store.ParticipantRow

func (d roomData) stateFor(token string) map[string]any  // BuildRoomState(d..., d.requester(token))
func (d roomData) stateForParticipant(p *store.ParticipantRow) map[string]any
```

- [ ] **Step 1: Failing test** `snapshot_test.go`:

```go
package server

import (
	"path/filepath"
	"testing"

	"github.com/HendersonT/quick-vote/internal/store"
)

func TestBroadcastQueryCountIndependentOfConnections(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, nil)
	if err := st.CreateVote(store.VoteRow{Slug: "r", Title: "R", Phase: "suggesting", Settings: `{"creditsPerOption":3,"maxSuggestionsPerUser":3,"suggestAdvanceMode":"manual","voteAdvanceMode":"manual","tiebreaker":"most-backers","revoteThresholdPct":33}`, CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// Register fake connections directly in the hub (no network needed).
	register := func(n int) {
		for i := 0; i < n; i++ {
			c := &wsConn{send: make(chan []byte, 64), slug: "r", done: make(chan struct{})}
			s.hub.add(c)
		}
	}
	register(1)
	before := st.QueryCount()
	s.broadcast("r")
	one := st.QueryCount() - before

	register(49)
	before = st.QueryCount()
	s.broadcast("r")
	fifty := st.QueryCount() - before

	if one != fifty {
		t.Fatalf("broadcast queries: 1 conn=%d, 50 conns=%d — must not scale with connections", one, fifty)
	}
}

func TestRoomDataRequesterConstantTimeMatch(t *testing.T) {
	d := roomData{parts: []store.ParticipantRow{{ID: "a", Token: "tok-a"}, {ID: "b", Token: "tok-b"}}}
	if p := d.requester("tok-b"); p == nil || p.ID != "b" {
		t.Fatalf("requester(tok-b) = %v", p)
	}
	if d.requester("") != nil || d.requester("nope") != nil {
		t.Fatal("empty/unknown token must resolve to spectator")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/server/ -run 'Broadcast|RoomData'` — FAIL (undefined).

- [ ] **Step 3: Implement** `snapshot.go` with the interface above (`requester` loops all parts using `subtle.ConstantTimeCompare`; returns a pointer to a copy). `broadcast(slug)`: `conns := s.hub.connsFor(slug)`; if none return; `d, err := s.loadRoom(slug)`; on error return; for each conn: marshal `d.stateFor(c.token)` and enqueue via a new `s.enqueue(c, data)` (the existing select/drop logic from `pushSnapshot`). `pushSnapshot(c)` (initial push) = `loadRoom` + `enqueue`. `handleGetVote` and `writeState` use `loadRoom` (writeState: `d.stateForParticipant(requester)`), keeping their existing 404/500 behavior.

- [ ] **Step 4: Run** `go test -race ./...` — PASS.

- [ ] **Step 5: Commit** — `perf: load room data once per broadcast`.

---

### Task 5: WebSocket auth by first message (spec A5)

**Files:**
- Modify: `internal/server/ws.go`, `internal/server/server.go` (`Config.WSAuthTimeout`), `internal/server/ws_test.go` (`dialWS` sends auth), `web/src/ws.ts`
- Test: `internal/server/ws_test.go` (new cases)

**Interfaces:**
- Produces: `Config.WSAuthTimeout time.Duration` (0 → 5 s). Client→server first message `{"type":"auth","token":"<session token or empty>"}`. Close codes: `websocket.ClosePolicyViolation` with reason `"auth required"` on timeout/garbage.
- Changes `wsConn.token` to be set from the auth message; the `?token=` query parameter is no longer read.

- [ ] **Step 1: Update `dialWS` and add failing tests.** `dialWS(t, ts, slug, token)` now dials without a query string and immediately writes `{"type":"auth","token":token}` (empty string for spectators). Add `dialRaw(t, ts, slug) *websocket.Conn` that dials without sending anything. `newWSTestServer` passes `server.Config{WSAuthTimeout: 200 * time.Millisecond}` via `NewWithConfig`. New tests:

```go
func TestWebSocketNoAuthMessageIsClosed(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	conn := dialRaw(t, ts, slug)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("want policy-violation close without auth, got %v", err)
	}
}

func TestWebSocketGarbageFirstMessageIsClosed(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	conn := dialRaw(t, ts, slug)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("want policy-violation close for wrong type, got %v", err)
	}
}

func TestWebSocketQueryTokenIgnored(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, _, tok, _ := createVote(t, s, nil)
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/votes/" + slug + "/ws?token=" + url.QueryEscape(tok)
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"type": "auth"})
	if snap := readSnapshot(t, conn); snap["you"] != nil {
		t.Fatal("a ?token= query parameter must not authenticate")
	}
}
```

Existing `TestWebSocketInitialSnapshots` / broadcast tests keep passing through the updated `dialWS`.

- [ ] **Step 2: Run** `go test ./internal/server/ -run WebSocket` — FAIL.

- [ ] **Step 3: Implement server side.** In `handleWS`, after upgrade (connection slot already reserved), do **not** add to the hub yet. Start a goroutine `s.wsAuth(c)`:

```go
// wsAuth waits for the client's auth message before the connection joins the
// hub. Tokens travel in a message rather than the URL so they never land in
// proxy or tunnel access logs.
func (s *Server) wsAuth(c *wsConn) {
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(s.wsAuthTimeout()))
	var msg struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := c.conn.ReadJSON(&msg); err != nil || msg.Type != "auth" {
		deadline := time.Now().Add(wsWriteWait)
		_ = c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "auth required"), deadline)
		c.conn.Close()
		s.hub.release(c.ip)
		return
	}
	c.token = msg.Token
	s.hub.add(c)
	go s.wsWritePump(c)
	s.pushSnapshot(c)
	s.wsReadPump(c) // existing loop: pong deadline, discard messages, remove on exit
}
```

`handleWS` no longer reads `r.URL.Query()`, and `wsReadPump`'s existing `SetReadLimit`/deadline setup stays (it resets the deadline to `wsPongWait`). Remove the old direct `go s.wsWritePump(c); go s.wsReadPump(c); s.pushSnapshot(c)` sequence. Check `hub.add` ordering vs. per-slug cap: the slot was reserved before upgrade, so behavior is unchanged.

- [ ] **Step 4: Implement client side** — `web/src/ws.ts`: drop the query string from `url`; in `socket.onopen` send `socket.send(JSON.stringify({ type: "auth", token }))` then reset backoff. Update the file's doc comment (token sent as first message, not in the URL).

- [ ] **Step 5: Run** `go test -race ./...` and `cd web && npm run build && npm test` — PASS.

- [ ] **Step 6: Commit** — `security: websocket session token sent as first message, not in URL`.

---

### Task 6: Remove participant (spec B1, server)

**Files:**
- Create: `internal/server/creator.go` (holds `requireCreator` + creator handlers for Tasks 6, 8, 9)
- Modify: `internal/store/store.go` (`RemoveParticipant`), `internal/server/server.go` (route), `internal/server/handlers.go` (`handleAdvance` uses `requireCreator`)
- Test: `internal/server/creator_test.go`, `internal/store/store_test.go`

**Interfaces:**
- Produces store: `var ErrIsCreator = errors.New("cannot remove the creator")`;
  `func (s *Store) RemoveParticipant(slug, participantID, newToken string, deleteOptions bool, at int64) error` — one transaction: verify the participant exists in `slug`, is not removed (else `ErrNotFound`), is not creator (else `ErrIsCreator`); `DELETE FROM ballots WHERE vote_slug=? AND participant_id=?`; if `deleteOptions`, `DELETE FROM options WHERE vote_slug=? AND participant_id=?`; `UPDATE participants SET removed_at=?, token=?, wants_revote=0, done_suggesting=0, next_token=NULL WHERE id=?`.
- Produces server:

```go
// requireCreator authenticates the caller as this vote's creator: a valid
// creator session AND the matching X-Creator-Token. Writes 401/403 and
// returns ok=false otherwise.
func (s *Server) requireCreator(w http.ResponseWriter, r *http.Request, v store.VoteRow) (store.ParticipantRow, bool)
```

  (401 via `requireParticipant`; 403 `"creator token required"` if the participant is not the creator **or** the header mismatches.) Route: `r.With(write...).Delete("/participants/{id}", s.handleRemoveParticipant)`.

- [ ] **Step 1: Failing tests** `creator_test.go` (package `server_test`):

```go
func TestRemoveParticipantWipesAndInvalidates(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, bobTok, "Bob's idea")
	bobID := participantID(t, getState(t, s, slug, bobTok))

	rec, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(st["participants"].([]any)); n != 1 {
		t.Fatalf("participants after remove = %d, want 1", n)
	}
	if n := len(st["options"].([]any)); n != 0 {
		t.Fatalf("suggest-phase removal must delete their options, have %d", n)
	}
	if got := getState(t, s, slug, bobTok); got["you"] != nil {
		t.Fatal("removed participant's token must no longer authenticate")
	}
	if rec, _ := suggest(t, s, slug, bobTok, "again"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("removed participant write: %d, want 401", rec.Code)
	}
}

func TestRemoveParticipantDuringVotingKeepsOptions(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, bobTok, "Bob's idea")
	suggest(t, s, slug, aliceTok, "Alice's idea")
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	putBallot(t, s, slug, aliceTok, map[string]int{ids["Bob's idea"]: 1})
	putBallot(t, s, slug, bobTok, map[string]int{ids["Alice's idea"]: 1})
	bobID := participantID(t, getState(t, s, slug, bobTok))

	rec, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d", rec.Code)
	}
	if n := len(st["options"].([]any)); n != 2 {
		t.Fatalf("voting-phase removal must keep options (others voted on them), have %d", n)
	}
	you := st["you"].(map[string]any)
	if b := you["ballot"].(map[string]any); b[ids["Bob's idea"]] != float64(1) {
		t.Fatalf("Alice's ballot changed: %v", b)
	}
}

func TestRemoveParticipantAuth(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, st := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	aliceID := st["you"].(map[string]any)["participantId"].(string)
	bobID := participantID(t, getState(t, s, slug, bobTok))
	path := "/api/votes/" + slug + "/participants/"

	if rec, _ := doHdr(t, s, http.MethodDelete, path+bobID, nil, "", ct); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d, want 401", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+aliceID, nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator session with creator token: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+bobID, nil, aliceTok, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong creator token: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+aliceID, nil, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("removing creator: %d, want 400", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+"nope", nil, aliceTok, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown participant: %d, want 404", rec.Code)
	}
}

func TestRemoveLastHoldoutTriggersAllVotedAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "all-voted"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1})
	bobID := participantID(t, getState(t, s, slug, bobTok))

	_, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if st["phase"] != "results" {
		t.Fatalf("phase = %v, want results once the only non-voter is removed", st["phase"])
	}
}

func TestRemovedParticipantWSDowngradesToSpectator(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	bobConn := dialWS(t, ts, slug, bobTok)
	readSnapshot(t, bobConn)
	bobID := participantID(t, getState(t, s, slug, bobTok))

	doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if snap := readSnapshot(t, bobConn); snap["you"] != nil {
		t.Fatalf("removed participant's live connection still personalized: %v", snap["you"])
	}
}
```

Add helper in `lifecycle_test.go`: `func participantID(t *testing.T, state map[string]any) string` returning `state["you"].(map[string]any)["participantId"].(string)`. Store test: `TestRemoveParticipantStore` covering `ErrIsCreator`, `ErrNotFound` for a second removal, and that `ParticipantByToken(old token)` returns `ErrNotFound`.

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement** `handleRemoveParticipant` in `creator.go`: lock slug → `getVoteOr404` → (closed check added in Task 8) → `requireCreator` → `id := chi.URLParam(r,"id")` → `deleteOptions := v.Phase == "suggesting"` → `s.store.RemoveParticipant(slug, id, ids.NewToken(), deleteOptions, s.now().Unix())` mapping `ErrIsCreator`→400 `"the creator can't be removed"`, `ErrNotFound`→404 `"participant not found"` → `settings, _ := parseSettings(v)`; `s.maybeAutoAdvanceSuggest(slug, settings)` if suggesting, `s.maybeAutoAdvanceVote(slug, settings)` if voting → `s.changed(slug)` → `s.writeState(w, slug, &creator)`. Switch `handleAdvance` to `requireCreator` (behavior-compatible; tightens "creator token but non-creator session" to 403).

- [ ] **Step 4: Run** `go test -race ./...` — PASS.

- [ ] **Step 5: Commit** — `feat: creator can remove a participant`.

---

### Task 7: Creator may delete any suggestion (spec B2)

**Files:**
- Modify: `internal/store/store.go` (`DeleteOptionAny`), `internal/server/handlers.go` (`handleDeleteSuggestion`)
- Test: `internal/server/creator_test.go`

**Interfaces:**
- Produces: `func (s *Store) DeleteOptionAny(slug, optionID string) error` (no owner filter; `ErrNotFound` if absent).
- Behavior: if the request carries `X-Creator-Token` and `requireCreator` succeeds, delete with `DeleteOptionAny`; otherwise the existing owner-only path. A present-but-wrong creator token → 403 (don't silently fall back).

- [ ] **Step 1: Failing test:**

```go
func TestCreatorDeletesAnySuggestion(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	_, st := suggest(t, s, slug, bobTok, "spam")
	id := optionTitleToID(t, st)["spam"]
	path := "/api/votes/" + slug + "/suggestions/" + id

	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("creator without creator token deletes others': %d, want 404 (owner-only path)", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong creator token: %d, want 403", rec.Code)
	}
	rec, st := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, ct)
	if rec.Code != http.StatusOK || len(st["options"].([]any)) != 0 {
		t.Fatalf("creator delete: %d options=%v", rec.Code, st["options"])
	}
}
```

- [ ] **Step 2–4:** run (FAIL) → implement → `go test -race ./...` (PASS).
- [ ] **Step 5: Commit** — `feat: creator can delete any suggestion`.

---

### Task 8: Close / reopen (spec B3, server)

**Files:**
- Modify: `internal/store/store.go` (`SetClosed`), `internal/server/creator.go`, `internal/server/handlers.go` (closed guard in every write handler), `internal/server/timers.go` (`timerFired` no-op when closed), `internal/server/state.go` (`closed` field), `internal/server/server.go` (routes)
- Test: `internal/server/creator_test.go`

**Interfaces:**
- Produces: `func (s *Store) SetClosed(slug string, closedAt *int64) error`.
- Produces: `func rejectIfClosed(w http.ResponseWriter, v store.VoteRow) bool` — writes 409 `"this vote is closed"` and returns true when `v.ClosedAt != nil`.
- Room state: `"closed": v.ClosedAt != nil`.
- Routes: `POST /close`, `POST /reopen` (both `write...`).

- [ ] **Step 1: Failing tests:**

```go
func TestCloseBlocksWritesAndReopenRestores(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	base := "/api/votes/" + slug

	rec, st := doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec.Code != http.StatusOK || st["closed"] != true {
		t.Fatalf("close: %d closed=%v", rec.Code, st["closed"])
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("close is idempotent: %d", rec.Code)
	}
	blocked := []struct{ method, path string; body any; tok, ct string }{
		{http.MethodPost, base + "/join", map[string]any{"name": "Carol"}, "", ""},
		{http.MethodPost, base + "/suggestions", map[string]any{"title": "x"}, bobTok, ""},
		{http.MethodPost, base + "/done-suggesting", map[string]any{}, bobTok, ""},
		{http.MethodPost, base + "/advance", map[string]any{}, aliceTok, ct},
	}
	for _, b := range blocked {
		if rec, _ := doHdr(t, s, b.method, b.path, b.body, b.tok, b.ct); rec.Code != http.StatusConflict {
			t.Errorf("%s %s while closed: %d, want 409", b.method, b.path, rec.Code)
		}
	}
	if rec, _ := doJSON(t, s, http.MethodGet, base, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET while closed: %d", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/close", nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator close: %d, want 403", rec.Code)
	}

	rec, st = doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)
	if rec.Code != http.StatusOK || st["closed"] != false {
		t.Fatalf("reopen: %d closed=%v", rec.Code, st["closed"])
	}
	if rec, _ := suggest(t, s, slug, bobTok, "after reopen"); rec.Code != http.StatusOK {
		t.Fatalf("suggest after reopen: %d", rec.Code)
	}
}

func TestClosedVoteTimerDoesNotAdvance(t *testing.T) {
	s, fc := newFakeClockServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 60})
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/close", nil, aliceTok, ct)

	fc.Advance(2 * time.Minute)
	st := getState(t, s, slug, aliceTok)
	if st["phase"] != "suggesting" || st["phaseDeadline"] != nil {
		t.Fatalf("closed vote changed on timer: phase=%v deadline=%v", st["phase"], st["phaseDeadline"])
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/reopen", nil, aliceTok, ct)
	fc.Advance(2 * time.Minute)
	if st := getState(t, s, slug, aliceTok); st["phase"] != "suggesting" {
		t.Fatalf("reopen must not re-arm the timer: phase=%v", st["phase"])
	}
}
```

Also:

```go
func TestCloseBlocksVotingAndResultsWrites(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	_, st := suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	optA := optionTitleToID(t, st)["A"]
	base := "/api/votes/" + slug
	bobID := participantID(t, getState(t, s, slug, bobTok))

	// Suggest phase: deleting a suggestion is blocked while closed.
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodDelete, base+"/suggestions/"+optA, nil, aliceTok, ""); rec.Code != http.StatusConflict {
		t.Fatalf("delete suggestion while closed: %d, want 409", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, base+"/participants/"+bobID, nil, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("remove participant while closed: %d, want 409", rec.Code)
	}
	doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)

	// Voting phase: ballots blocked.
	doHdr(t, s, http.MethodPost, base+"/advance", map[string]any{}, aliceTok, ct)
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := putBallot(t, s, slug, bobTok, map[string]int{optA: 1}); rec.Code != http.StatusConflict {
		t.Fatalf("ballot while closed: %d, want 409", rec.Code)
	}
	doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)

	// Results phase: re-vote calls blocked.
	doHdr(t, s, http.MethodPost, base+"/advance", map[string]any{}, aliceTok, ct)
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/revote", map[string]any{}, bobTok, ""); rec.Code != http.StatusConflict {
		t.Fatalf("revote while closed: %d, want 409", rec.Code)
	}
}
```

(The `/next`-while-closed case lives in Task 9's `TestNextVoteValidationAndClosed`.)

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement.** `handleClose`: lock → vote → `requireCreator` → if not closed: `SetClosed(slug, &now)`, clear stored deadline (`v.PhaseDeadline=nil; UpdateVote`) and `s.scheduler.Clear(slug)` → `changed` → state. `handleReopen`: same auth, `SetClosed(slug, nil)`, no timer → `changed` → state. Add `if rejectIfClosed(w, v) { return }` right after `getVoteOr404` in: join, create/delete suggestion, done-suggesting, ballot, advance, revote, remove participant. `timerFired`: after locking, load the vote and return early if closed (and don't call `changed`). `BuildRoomState`: add `"closed"`.

- [ ] **Step 4: Run** `go test -race ./...` — PASS.
- [ ] **Step 5: Commit** — `feat: creator can close and reopen a vote`.

---

### Task 9: Next vote with the same group (spec B4, server)

**Files:**
- Modify: `internal/store/store.go` (`CreateNextVote`), `internal/server/creator.go` (`handleNextVote`), `internal/server/state.go` (`next`, `you.nextSessionToken`, `you.nextCreatorToken`), `internal/server/server.go` (route with `createLimit`/`createGlobal`), `internal/server/handlers.go` (extract shared `buildNewVote` from `handleCreateVote` if it avoids duplication)
- Test: `internal/server/creator_test.go`, `internal/store/store_test.go`

**Interfaces:**
- Produces store:

```go
// ErrConflict signals a state conflict (e.g. a successor vote already exists).
var ErrConflict = errors.New("conflict")

// CreateNextVote atomically creates next (with its participants) and links
// it from srcSlug: sets votes.next_slug and next_creator_token on the source
// and participants.next_token for each carried-over source participant.
// carried maps source participant ID -> new participant row. Returns
// ErrConflict if the source already has a successor.
func (s *Store) CreateNextVote(srcSlug string, next VoteRow, carried map[string]ParticipantRow) error
```

- Produces HTTP: `POST /api/votes/{slug}/next` body `{title string, settings *settingsPatch}` → 201 `createVoteResponse` for the caller (the new vote's creator session + creator token + state). 409 if closed or `next_slug` set. Title rules identical to create (trimmed, 1–200). Settings: `parseSettings(v)` then `req.Settings.applyTo(...)`, `Validate`.
- Room state: `"next": {"slug","title"}` or `null`; `you.nextSessionToken` (string, omitted when nil) and, for the creator, `you.nextCreatorToken`.

- [ ] **Step 1: Failing tests:**

```go
func TestNextVoteCarriesGroupWithFreshTokens(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"creditsPerOption": 7})
	bobTok := join(t, s, slug, "Bob")

	rec, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "Round two"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next: %d %s", rec.Code, rec.Body.String())
	}
	next := out["slug"].(string)
	newState := out["state"].(map[string]any)
	if newState["title"] != "Round two" || newState["settings"].(map[string]any)["creditsPerOption"] != float64(7) {
		t.Fatalf("next vote title/settings not carried: %v", newState)
	}
	if n := len(newState["participants"].([]any)); n != 2 {
		t.Fatalf("carried participants = %d, want 2", n)
	}

	bobOld := getState(t, s, slug, bobTok)
	if bobOld["next"].(map[string]any)["slug"] != next {
		t.Fatalf("old vote next = %v", bobOld["next"])
	}
	bobYou := bobOld["you"].(map[string]any)
	bobNewTok, _ := bobYou["nextSessionToken"].(string)
	if bobNewTok == "" || bobNewTok == bobTok {
		t.Fatalf("Bob needs a fresh token, got %q", bobNewTok)
	}
	if _, ok := bobYou["nextCreatorToken"]; ok {
		t.Fatal("non-creator must not receive the next creator token")
	}
	if got := getState(t, s, next, bobNewTok); got["you"] == nil {
		t.Fatal("Bob's next token doesn't authenticate in the new vote")
	}
	if got := getState(t, s, next, bobTok); got["you"] != nil {
		t.Fatal("old token must not authenticate in the new vote")
	}
	// Spectators see the link but no tokens.
	spec := getState(t, s, slug, "")
	if spec["next"] == nil || spec["you"] != nil {
		t.Fatalf("spectator view: next=%v you=%v", spec["next"], spec["you"])
	}
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "again"}, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("second successor: %d, want 409", rec.Code)
	}
}

func TestNextVoteSkipsRemovedParticipants(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	join(t, s, slug, "Carol")
	bobID := participantID(t, getState(t, s, slug, bobTok))
	doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)

	_, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	names := []string{}
	for _, p := range out["state"].(map[string]any)["participants"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if len(names) != 2 || names[0] != "Alice" || names[1] != "Carol" {
		t.Fatalf("carried %v, want [Alice Carol]", names)
	}
}

func TestNextVoteValidationAndClosed(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	path := "/api/votes/" + slug + "/next"
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "  "}, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank title: %d, want 400", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x", "settings": map[string]any{"creditsPerOption": 0}}, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings: %d, want 400", rec.Code)
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x"}, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("next on closed vote: %d, want 409", rec.Code)
	}
}
```

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement** `handleNextVote`: lock source slug → vote → `rejectIfClosed` → `requireCreator` → if `v.NextSlug != nil` 409 `"this vote already has a follow-up"` → decode/validate title + settings → `parts := s.store.Participants(slug)` (removed already excluded; names already unique) → build new `VoteRow` (new slug, new creator token, timer deadline as in create using `s.now()`), and `carried[old.ID] = ParticipantRow{ID: ids.NewToken(), VoteSlug: newSlug, Name: old.Name, Token: ids.NewToken(), IsCreator: old.IsCreator, JoinedAt: now}` → `CreateNextVote` (map `ErrConflict`→409) → `s.armOrClear(newSlug, deadline)` → `s.changed(slug)` (old room broadcasts the handoff) → respond 201 with the caller's carried participant. In `BuildRoomState`, when `v.NextSlug != nil`, load is already in memory? The next title isn't — **add `NextTitle *string` to `VoteRow`** populated by `GetVote` via `LEFT JOIN votes n ON n.slug = v.next_slug` (select `n.title`). Personalized `you` adds `nextSessionToken` from `requester.NextToken`, and `nextCreatorToken` from `v.NextCreatorToken` only when `requester.IsCreator`.

- [ ] **Step 4: Run** `go test -race ./...` — PASS.
- [ ] **Step 5: Commit** — `feat: start another vote with the same group`.

---

### Task 10: Results CSV export (spec C1, server)

**Files:**
- Create: `internal/server/export.go`
- Modify: `internal/server/server.go` (route `r.Get("/results.csv", s.handleResultsCSV)` inside `/{slug}`)
- Test: `internal/server/export_test.go`

**Interfaces:**
- Produces: `GET /api/votes/{slug}/results.csv`; `func csvSafe(s string) string`.

- [ ] **Step 1: Failing tests** — drive a vote to results with titles `=HYPERLINK("x")`, `+1`, `-cmd`, `@SUM(A1)`, `Plain, with "quotes"`, and `multi\nline`; ballots giving deterministic scores:

```go
func TestResultsCSVNeutralizesFormulas(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual", "maxSuggestionsPerUser": 6})
	titles := []string{`=HYPERLINK("x")`, "+1", "-cmd", "@SUM(A1)", `Plain, with "quotes"`, "multi\nline"}
	for _, ti := range titles {
		if rec, _ := suggest(t, s, slug, aliceTok, ti); rec.Code != http.StatusOK {
			t.Fatalf("suggest %q: %d", ti, rec.Code)
		}
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	putBallot(t, s, slug, aliceTok, map[string]int{ids[`=HYPERLINK("x")`]: 2})
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)

	req := httptest.NewRequest(http.MethodGet, "/api/votes/"+slug+"/results.csv", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, slug+"-results.csv") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if strings.Join(rows[0], ",") != "rank,option,score,backers,vetoes,eliminated,winner" {
		t.Fatalf("header = %v", rows[0])
	}
	if rows[1][1] != `'=HYPERLINK("x")` || rows[1][6] != "yes" || rows[1][0] != "1" {
		t.Fatalf("top row = %v", rows[1])
	}
	for _, r := range rows[1:] {
		if c := r[1][0]; c == '=' || c == '+' || c == '-' || c == '@' {
			t.Fatalf("formula-leading cell survived: %q", r[1])
		}
	}
	joined := rec.Body.String()
	if strings.Contains(joined, aliceTok) || strings.Contains(joined, "Alice") {
		t.Fatal("CSV must not include participant tokens or names")
	}
}

func TestResultsCSVBeforeResults(t *testing.T) {
	s := newTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/votes/"+slug+"/results.csv", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("csv before results: %d, want 409", rec.Code)
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `export.go`: load vote (404), require `v.Results != nil` (409 `"results aren't in yet"`), decode `domain.Results`, map option IDs → titles via `s.store.Options`, write with `encoding/csv`; `rank` = 1-based position in `Scores`; `eliminated`/`winner` = `yes`/`no` (`winner` true iff `optionId == WinnerID && !TiePending`). `csvSafe`: if the first rune is one of `= + - @ \t \r`, prefix `'`. Headers: `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="<slug>-results.csv"`.
- [ ] **Step 4: Run** `go test -race ./...` — PASS.
- [ ] **Step 5: Commit** — `feat: results CSV export (totals only, formula-safe)`.

---

### Task 11: Frontend toolchain upgrade (spec D3)

**Files:** Modify `web/package.json`, `web/package-lock.json`, `web/vite.config.ts` / `web/tsconfig.json` only if the new majors require it.

- [ ] **Step 1:** `cd web && npm install -D vite@^8 vitest@^5 @vitejs/plugin-react@^6 && npm install qrcode@^1.5 && npm install -D @types/qrcode@^1.5`.
- [ ] **Step 2:** `npm run build && npm test && npm audit` — Expected: build + 14 tests pass; `found 0 vulnerabilities`. If a major needs config changes (e.g. Vitest `test` config or Node engine), apply the documented migration and note it in the commit body. If `npm audit` still reports issues that require unreleased fixes, record them in the commit body instead of forcing.
- [ ] **Step 3:** `cd .. && docker build -q -t quickvote:t11 .` — Expected: success (node:24-alpine stage).
- [ ] **Step 4: Commit** — `build: upgrade Vite/Vitest/plugin-react; add qrcode`.

---

### Task 12: Frontend — creator controls, closed state, removed notice, next-vote handoff (spec B1–B4 UI)

**Files:**
- Modify: `web/src/types.ts`, `web/src/api.ts`, `web/src/session.ts`, `web/src/pages/Room.tsx`, `web/src/pages/Home.tsx` (extract settings + history labels), `web/src/components/ParticipantList.tsx`, `web/src/components/SuggestPhase.tsx`, `web/src/components/JoinGate.tsx` (notice prop), `web/src/App.tsx` (`key={slug}`), `web/src/styles.css`
- Create: `web/src/components/ConfirmButton.tsx`, `web/src/components/SettingsFields.tsx`, `web/src/components/NextVoteForm.tsx`, `web/src/session.test.ts`

**Interfaces:**
- `types.ts`: `RoomState.closed: boolean`; `RoomState.next: { slug: string; title: string } | null`; `You.nextSessionToken?: string`; `You.nextCreatorToken?: string`.
- `api.ts`: `removeParticipant(slug, sessionToken, creatorToken, participantId): Promise<RoomState>`; `deleteSuggestion(slug, sessionToken, optionId, creatorToken?)`; `closeVote(slug, sessionToken, creatorToken)`; `reopenVote(...)`; `createNextVote(slug, sessionToken, creatorToken, title, settings?): Promise<CreateVoteResponse>`.
- `session.ts`:

```ts
export interface HistoryEntry { slug: string; title: string; ts: number; closed?: boolean }
/** Records the last-seen closed flag without reordering (no-op if unchanged/absent). */
export function setHistoryClosed(slug: string, closed: boolean): void
/** Open votes first (most recent first), then closed ones (most recent first). */
export function sortHistory(entries: HistoryEntry[]): HistoryEntry[]
/** True when this browser should auto-follow the group to `state.next` —
 * only once per old slug, so revisiting an old vote never redirect-loops. */
export function shouldAutoMove(oldSlug: string, state: RoomState): boolean
/** Saves the next-vote session, marks oldSlug as moved, records history. */
export function applyMove(oldSlug: string, state: RoomState, name: string): string /* next slug */
```

  (`shouldAutoMove` = `!!state.next && !!state.you?.nextSessionToken && localStorage.getItem("qv:moved:"+oldSlug) === null`.)
- `SettingsFields`: `props { value: Settings-like form state; onChange(next) }` containing the exact inputs currently inline in `Home.tsx` (advanced `<details>` included); `Home.tsx` refactored to use it with no visual change.
- `ConfirmButton`: `props { label; confirmLabel?: string ("Yes"); onConfirm: () => Promise<void> | void; disabled?; className? }` — first click swaps to "Sure? Yes / Cancel" inline.

- [ ] **Step 1: Failing Vitest** `session.test.ts` (jsdom not required — stub `localStorage` with a Map-backed object in `beforeEach` via `vi.stubGlobal`):

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyMove, getSession, setHistoryClosed, addToHistory, getHistory, shouldAutoMove, sortHistory } from "./session";
import type { RoomState } from "./types";

function memStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

const moved = (over: Partial<RoomState> = {}) =>
  ({ slug: "old", title: "Old", next: { slug: "new", title: "New" },
     you: { participantId: "p", isCreator: false, ballot: null, nextSessionToken: "tok2" }, ...over }) as unknown as RoomState;

describe("next-vote handoff", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  it("auto-moves once, then never again for the same old vote", () => {
    expect(shouldAutoMove("old", moved())).toBe(true);
    expect(applyMove("old", moved(), "Bob")).toBe("new");
    expect(getSession("new")?.sessionToken).toBe("tok2");
    expect(shouldAutoMove("old", moved())).toBe(false);
  });

  it("does not move spectators or when there is no successor", () => {
    expect(shouldAutoMove("old", moved({ you: null } as Partial<RoomState>))).toBe(false);
    expect(shouldAutoMove("old", moved({ next: null } as Partial<RoomState>))).toBe(false);
  });

  it("carries the creator token for the creator", () => {
    const st = moved({ you: { participantId: "p", isCreator: true, ballot: null, nextSessionToken: "t", nextCreatorToken: "c" } } as Partial<RoomState>);
    applyMove("old", st, "Alice");
    expect(getSession("new")?.creatorToken).toBe("c");
  });
});

describe("history closed flag", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  it("sorts open votes before closed ones", () => {
    addToHistory("a", "A");
    addToHistory("b", "B");
    setHistoryClosed("b", true);
    expect(sortHistory(getHistory()).map((e) => e.slug)).toEqual(["a", "b"]);
  });
});
```

- [ ] **Step 2: Run** `cd web && npx vitest run` — FAIL (missing exports).
- [ ] **Step 3: Implement** `session.ts` helpers, `types.ts`, `api.ts`.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: UI.**
  - `App.tsx`: `<Room key={route.slug} slug={route.slug} />` (fresh state per vote).
  - `Room.tsx`:
    - On each snapshot: `setHistoryClosed(slug, next.closed)`. If `shouldAutoMove(slug, next)`: `const to = applyMove(slug, next, session.name)`; `sessionStorage.setItem("qv:notice", "Moved to the next vote: " + next.next!.title)` (wrap storage calls in try/catch); `navigate("/v/" + to)`; return.
    - Replace the `you === null` branch: keep clearing the session, and pass `notice="You're no longer in this vote — you can join again below."` to `JoinGate` (new optional prop rendered as an info banner above the form).
    - On mount, read-and-remove `qv:notice` from sessionStorage and render it as a dismissible banner.
    - If `state.closed`: render `<p className="closed-banner" role="status">This vote is closed.</p>` under the header; pass `closed` to phase components to disable their inputs/buttons.
    - If `state.next` and not auto-moving: banner `This group moved on → <a href="/v/{next.slug}">{next.title}</a>` (navigate on click).
    - Creator controls: existing Advance (hidden while closed); `ConfirmButton` "Close vote" / plain "Reopen" button; "Start another vote with this group" toggles `NextVoteForm` (hidden when `state.next` is set or closed). Replace the existing `window.confirm` in `handleAdvance` with `ConfirmButton` for consistency.
  - `ParticipantList`: new props `canRemove: boolean`, `onRemove(id)`; show a `ConfirmButton` "Remove" (label `×`, `aria-label="Remove {name}"`) on non-creator rows when `canRemove`.
  - `SuggestPhase`: creator sees a delete `ConfirmButton` on every option (passes `creatorToken`); `participantName` fallback text becomes `"removed participant"`; add `closed` prop disabling the form.
  - `VotePhase`, `ResultsPhase`: `closed` prop disables submit / re-vote / tiebreak buttons.
  - `NextVoteForm`: title input (required, ≤200) + `SettingsFields` initialized from `state.settings` + "Create" button → `createNextVote` → `saveSession(newSlug, {sessionToken, creatorToken, name})`, `applyMove`-equivalent marking of the old slug (`localStorage "qv:moved:"+slug`), `addToHistory`, `navigate`.
  - `Home.tsx`: use `SettingsFields`; render `sortHistory(getHistory())` with a `<span className="history-closed">closed</span>` label on closed entries.
  - `styles.css`: `.closed-banner`, `.info-banner`, `.confirm-button` styles matching existing banner/button styles, light and dark.
- [ ] **Step 6: Run** `npm run build && npm test` — PASS; `go test ./...` still PASS.
- [ ] **Step 7: Commit** — `feat(web): creator controls, closed state, removed notice, next-vote handoff`.

---

### Task 13: Frontend — results page, CSV/results-link buttons, QR code (spec C1–C3 UI)

**Files:**
- Create: `web/src/pages/ResultsView.tsx`, `web/src/components/QrCode.tsx`
- Modify: `web/src/App.tsx` (route), `web/src/components/ResultsPhase.tsx` (`readOnly` prop, CSV + copy-results-link buttons), `web/src/components/ShareLink.tsx` (QR toggle), `web/src/styles.css`
- Test: `web/src/route.test.ts` (export `parseRoute` from `App.tsx` for testing)

**Interfaces:**
- `parseRoute`: adds `{ name: "results"; slug: string }` for `/v/:slug/results` (trailing slash allowed). Exported.
- `ResultsPhase` props gain `readOnly?: boolean` (hides re-vote panel and tiebreak buttons) and `closed?: boolean`.
- `QrCode`: `props { url: string }` → renders `<img alt="QR code for the join link" src={dataUrl} width=192 height=192>` using `QRCode.toDataURL(url, { margin: 1, width: 192 })` in an effect.

- [ ] **Step 1: Failing Vitest** `route.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseRoute } from "./App";

describe("parseRoute", () => {
  it("routes results pages", () => {
    expect(parseRoute("/v/AbC123xyZ9/results")).toEqual({ name: "results", slug: "AbC123xyZ9" });
    expect(parseRoute("/v/AbC123xyZ9/results/")).toEqual({ name: "results", slug: "AbC123xyZ9" });
  });
  it("keeps room and home routes", () => {
    expect(parseRoute("/v/abc")).toEqual({ name: "room", slug: "abc" });
    expect(parseRoute("/")).toEqual({ name: "home" });
    expect(parseRoute("/v/abc/other")).toEqual({ name: "not-found" });
  });
});
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.**
  - `ResultsView`: `getVote(slug)` (spectator) then `connectRoom(slug, "", setState)` for live updates. Renders title, phase badge, closed banner, then: results → `<ResultsPhase readOnly state=… slug=… sessionToken="" />`; otherwise "Results aren't in yet" + phase summary. If open and not in results: link "Join this vote" → `/v/{slug}`. Unknown slug → existing not-found styling.
  - `ResultsPhase` (when `results` present): `<a className="button-link" href={`/api/votes/${slug}/results.csv`}>Download CSV</a>` and a "Copy results link" button copying `${origin}/v/${slug}/results` (same clipboard fallback as `ShareLink`).
  - `ShareLink`: add "Show QR"/"Hide QR" toggle rendering `<QrCode url={url} />` beneath.
- [ ] **Step 4: Run** `npm run build && npm test` — PASS.
- [ ] **Step 5: Commit** — `feat(web): read-only results page, CSV download, QR code`.

---

### Task 14: CI and Dependabot (spec D1, D2)

**Files:** Create `.github/workflows/ci.yml`, `.github/dependabot.yml`.

- [ ] **Step 1: Write** `ci.yml`:

```yaml
name: CI

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...

  web:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: web
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-node@v5
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: npm ci
      - run: npx tsc --noEmit
      - run: npx vitest run
      - run: npx vite build

  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - run: docker build .
```

Before committing, confirm the current major of each action (`gh api repos/actions/checkout/releases/latest --jq .tag_name`, same for `setup-go`, `setup-node`) and use those majors.

`dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: { interval: weekly }
    groups:
      go-minor: { update-types: [minor, patch] }
  - package-ecosystem: npm
    directory: /web
    schedule: { interval: weekly }
    groups:
      npm-minor: { update-types: [minor, patch] }
  - package-ecosystem: docker
    directory: /
    schedule: { interval: weekly }
  - package-ecosystem: github-actions
    directory: /
    schedule: { interval: weekly }
```

- [ ] **Step 2: Validate** — `python3 -c "import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]" .github/workflows/ci.yml .github/dependabot.yml` (no output = OK). `go` job note: `setup-go` with `go-version-file: go.mod` selects Go 1.23 (the `go` directive) — the `toolchain` line in `go.mod` governs; if CI Go is older than a dependency needs, raise only the `toolchain` directive, never `go`.
- [ ] **Step 3: Commit** — `ci: GitHub Actions for Go, web and Docker; Dependabot`.

---

### Task 15: README and screenshots (spec D4)

**Files:** Modify `README.md`; create `docs/screenshots/{create,voting,results}.png`, `scripts/screenshots.mjs` (dev-only, not built into the image — add `scripts/` to `.dockerignore`).

- [ ] **Step 1: Screenshot script.** `scripts/screenshots.mjs` uses the `playwright` npm package (run via `npx -y playwright@<version>` whose bundled Chromium revision matches `~/.cache/ms-playwright/chromium-1234`; if none matches, `npx playwright install chromium`). It starts `go run ./cmd/quickvote -addr 127.0.0.1:18090 -db <tmp>/demo.db`, seeds via the API (vote "Friday game night"; participants Alice/Bob/Carol; suggestions Catan, Wingspan, Azul, Codenames; ballots giving a clear winner), and captures at 1200×800, light theme: the create page, the voting screen (Bob's context), and the results screen. Kills the server and deletes the temp DB afterwards.
- [ ] **Step 2: README.** Add at top: CI badge `![CI](https://github.com/HendersonT/quick-vote/actions/workflows/ci.yml/badge.svg)`, one-paragraph "What is quadratic voting" (spreading credits is cheap, stacking them is expensive, so strong minority preferences count without letting one voice dominate), live demo link `https://vote.aakster.net` (note: public demo, votes deleted after 90 days idle), the three screenshots. Update the API section with the new endpoints (spec "API summary") and document the WebSocket auth message, close/reopen, next vote, CSV, results page.
- [ ] **Step 3: Verify** links/paths render: `grep -o 'docs/screenshots/[a-z]*.png' README.md | xargs ls`.
- [ ] **Step 4: Commit** — `docs: README demo, screenshots, new endpoints`.

---

## Controller-only steps (after Task 15 — not dispatched as SDD tasks)

1. **Whole-branch review** (spec compliance, correctness/security, minor triage), one fix wave, scoped re-review.
2. **End-to-end browser check** (spec Testing): build image `quickvote:e2e`, run on `127.0.0.1:18091` with a tmpfs DB, drive two Playwright contexts through create → join → suggest → vote (live updates) → QR renders → close/reopen → remove participant (second context sees notice) → next vote (second context auto-moves exactly once; revisiting old slug shows the banner, no loop) → CSV download → `/v/{slug}/results`. Fail on any console error or CSP violation.
3. **Backups (D5, host):** `/opt/services/vote/ops/backup.sh` modeled on `/opt/services/hub/ops/backup.sh`: `umask 027`; `docker run --rm -v /opt/services/vote/data/db:/d:ro -v /opt/services/vote/data/backups:/o alpine:3.24 sh -c "apk add -q sqlite && sqlite3 'file:/d/quickvote.db?mode=ro' \".backup '/o/quickvote-$(date +%F).db'\""`; prune `-mtime +30`. Crontab line `20 2 * * * /opt/services/vote/ops/backup.sh`. Run once and verify the snapshot opens and counts votes.
4. **Merge + deploy:** merge `publish-round` → `master`, push to GitHub, confirm CI green; DB snapshot; `git fetch && git reset --hard origin/master` in `/opt/services/vote/app`; `docker compose build` (without piping away the exit status); `docker compose up -d`; verify live (headers, old link, WS auth, results page, CSV); update notes + memory.
