# Quick Vote Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A self-hostable containerized quadratic-voting app: create a vote, share a link, suggest options, vote quadratically, score with veto/tiebreak/re-vote — per `docs/superpowers/specs/2026-07-17-quick-vote-design.md`.

**Architecture:** Single Go binary (chi + gorilla/websocket + modernc.org/sqlite) embedding a Vite-built React SPA via `embed.FS`. All state in SQLite at `/data/quickvote.db`. Clients render one per-participant JSON room-state snapshot, pushed over WebSocket on every change.

**Tech Stack:** Go 1.22+, chi v5, gorilla/websocket, modernc.org/sqlite, React 18 + TypeScript + Vite, Vitest, Docker multi-stage.

---

## File structure

```
cmd/quickvote/main.go        flags (-addr, -db), wire store+server, serve
internal/ids/ids.go          NewSlug()/NewToken() via crypto/rand (+_test)
internal/domain/types.go     Phase, Tiebreaker, Settings, DefaultSettings()
internal/domain/ballot.go    BallotCost, ValidateBallot (+_test)
internal/domain/scoring.go   ComputeResults (+_test)
internal/domain/revote.go    RevoteNeeded, RevoteMet (+_test)
internal/store/store.go      SQLite open/schema/CRUD (+_test)
internal/server/server.go    New(store) *Server, chi routes, SPA fallback
internal/server/handlers.go  REST handlers (+handlers_test.go lifecycle tests)
internal/server/state.go     BuildRoomState(vote, forParticipant) snapshot
internal/server/ws.go        per-vote hub; Broadcast(slug) → per-conn snapshot
internal/server/timers.go    deadline scheduler → advancePhase
internal/server/advance.go   advancePhase(): rules, scoring on entry to results
webembed/embed.go            //go:embed all:dist  (web build copied here)
web/                         Vite app (see Task 9 for src layout)
Dockerfile  docker-compose.yml  .dockerignore  README.md
```

## Shared contract: room-state snapshot

One JSON shape, defined once in Go (`state.go`) and mirrored in `web/src/types.ts`. **Every field name below is normative.**

```jsonc
{
  "slug": "x7Kp2q",
  "title": "Friday game night",
  "phase": "suggesting",            // "suggesting" | "voting" | "results"
  "phaseDeadline": null,             // RFC3339 string or null
  "settings": {
    "maxSuggestionsPerUser": 3,
    "creditsPerOption": 3,
    "suggestAdvanceMode": "manual",  // "manual" | "count"
    "suggestAdvanceCount": 0,        // used when mode == "count"
    "voteAdvanceMode": "all-voted",  // "manual" | "all-voted"
    "survivalThreshold": 1,
    "tiebreaker": "most-backers",    // "most-backers" | "random" | "creator"
    "revoteThresholdPct": 33,
    "suggestTimerSecs": 0,           // 0 = off
    "voteTimerSecs": 0
  },
  "participants": [                  // joined order
    {"id": "p1", "name": "Sam", "isCreator": true,
     "hasSuggested": true, "hasVoted": false, "wantsRevote": false}
  ],
  "options": [                       // created order
    {"id": "o1", "title": "Catan", "suggestedById": "p1"}
  ],
  "budget": 9,                       // creditsPerOption * len(options)
  "you": {                           // null when requester not joined
    "participantId": "p1", "isCreator": true,
    "ballot": {"o1": 2}              // your saved votes, null if none
  },
  "results": null,                   // non-null only in results phase:
  // {"scores":[{"optionId":"o1","score":5,"backers":2,"eliminated":false}], // score desc
  //  "winnerOptionId":"o1",         // "" when none or tie pending
  //  "tiePending":false, "tiedOptionIds":[], "tiebreakNote":"",
  //  "revoteCalls":1, "revoteNeeded":2}
}
```

## API (all under `/api`, JSON; errors: `{"error":"human message"}` with 4xx)

| Method/path | Auth | Body → Response |
|---|---|---|
| POST `/votes` | — | `{title, creatorName, settings?}` (partial settings merged over defaults) → 201 `{slug, creatorToken, sessionToken, state}` |
| GET `/votes/{slug}` | optional Bearer | → state snapshot (`you` null if no/invalid token) |
| POST `/votes/{slug}/join` | — | `{name}` → `{sessionToken, state}`; dup names → `Sam (2)` |
| POST `/votes/{slug}/suggestions` | Bearer | `{title}` → state. 409 if wrong phase/cap hit/dup title (case-insens.) |
| DELETE `/votes/{slug}/suggestions/{id}` | Bearer | own suggestion, suggesting phase only → state |
| PUT `/votes/{slug}/ballot` | Bearer | `{votes:{optionId:int}}` → state. 409 wrong phase, 400 invalid/over budget |
| POST `/votes/{slug}/advance` | Bearer + `X-Creator-Token` | `{}` or `{winnerOptionId}` (creator tiebreak) → state. 409 if <2 options when leaving suggesting |
| POST `/votes/{slug}/revote` | Bearer | `{}` toggles caller's wantsRevote → state (may re-enter voting) |
| GET `/votes/{slug}/ws` | token as `?token=` query param | WebSocket; server pushes full personalized snapshot on connect and on every room change |

Auth: `Authorization: Bearer <sessionToken>` identifies the participant. Creator-only routes additionally require header `X-Creator-Token`. Unknown slug → 404 `{"error":"vote not found"}`.

## Phase/advance rules (implemented in `advance.go`, one code path)

`advancePhase(slug, byCreator bool, tiebreakWinner string)`:
- suggesting→voting: requires ≥2 options (else 409 "need at least 2 suggestions"); sets deadline from `voteTimerSecs` if >0.
- voting→results: computes `ComputeResults` with a `math/rand` source seeded from `crypto/rand`; stores results JSON; clears deadline. If `tiePending` and creator later POSTs advance with `winnerOptionId` (must be in `tiedOptionIds`), set winner.
- Auto-triggers, checked after each relevant mutation: suggest-count rule (distinct participants with ≥1 suggestion ≥ N), all-voted rule (every participant hasVoted), timer expiry (timers.go). Timer expiring on suggesting with <2 options: keep phase, clear deadline (creator must resolve).
- Re-vote trigger (in revote handler): if phase==results and `RevoteMet(calls, len(participants), pct)` → delete all ballots, reset all wantsRevote, results=NULL, phase=voting, restart vote timer if configured.

---

### Task 1: Repo scaffold + ids package

**Files:** Create `go.mod` (module `github.com/quickvote/quickvote`, go 1.22), `internal/ids/ids.go`, `internal/ids/ids_test.go`, `.gitignore` (`web/node_modules`, `web/dist`, `webembed/dist`, `*.db`).

- [ ] **Step 1: Failing test** — `ids_test.go`: `TestNewSlug` (len 6, alphabet `[A-Za-z0-9]`, 1000 draws no dup), `TestNewToken` (len 32 hex, unique).
- [ ] **Step 2: Run** `go test ./internal/ids/` — FAIL (undefined).
- [ ] **Step 3: Implement**

```go
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

const slugAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

func NewSlug() string {
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(slugAlphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = slugAlphabet[n.Int64()]
	}
	return string(b)
}

func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
```

(Adjust the slug test to the confusable-free alphabet above.)
- [ ] **Step 4: Run** — PASS. **Step 5: Commit** `feat: scaffold module and ids package`.

### Task 2: Domain — types, ballot validation

**Files:** Create `internal/domain/types.go`, `internal/domain/ballot.go`, `internal/domain/ballot_test.go`.

- [ ] **Step 1: types.go** (no test needed — declarations only)

```go
package domain

type Phase string

const (
	PhaseSuggesting Phase = "suggesting"
	PhaseVoting     Phase = "voting"
	PhaseResults    Phase = "results"
)

type Tiebreaker string

const (
	TiebreakMostBackers Tiebreaker = "most-backers"
	TiebreakRandom      Tiebreaker = "random"
	TiebreakCreator     Tiebreaker = "creator"
)

type Settings struct {
	MaxSuggestionsPerUser int        `json:"maxSuggestionsPerUser"`
	CreditsPerOption      int        `json:"creditsPerOption"`
	SuggestAdvanceMode    string     `json:"suggestAdvanceMode"`
	SuggestAdvanceCount   int        `json:"suggestAdvanceCount"`
	VoteAdvanceMode       string     `json:"voteAdvanceMode"`
	SurvivalThreshold     int        `json:"survivalThreshold"`
	Tiebreaker            Tiebreaker `json:"tiebreaker"`
	RevoteThresholdPct    int        `json:"revoteThresholdPct"`
	SuggestTimerSecs      int        `json:"suggestTimerSecs"`
	VoteTimerSecs         int        `json:"voteTimerSecs"`
}

func DefaultSettings() Settings {
	return Settings{
		MaxSuggestionsPerUser: 3,
		CreditsPerOption:      3,
		SuggestAdvanceMode:    "manual",
		VoteAdvanceMode:       "all-voted",
		SurvivalThreshold:     1,
		Tiebreaker:            TiebreakMostBackers,
		RevoteThresholdPct:    33,
	}
}

// Validate clamps/errors: MaxSuggestionsPerUser 1..20, CreditsPerOption 1..100,
// SurvivalThreshold >= 0, RevoteThresholdPct 1..100, timers 0..86400,
// modes/tiebreaker must be one of the enumerated strings.
func (s Settings) Validate() error { /* return fmt.Errorf on first violation */ }
```

- [ ] **Step 2: Failing tests** — `ballot_test.go` table tests:
  - `BallotCost({a:2,b:1})==5`; `BallotCost({})==0`.
  - `ValidateBallot` cases: ok at exact budget; over budget → err containing "budget"; negative vote → err; unknown option key → err; empty ballot ok (cost 0).
- [ ] **Step 3: Run** `go test ./internal/domain/` — FAIL.
- [ ] **Step 4: Implement**

```go
package domain

import "fmt"

func BallotCost(votes map[string]int) int {
	total := 0
	for _, v := range votes {
		total += v * v
	}
	return total
}

// ValidateBallot checks votes against the option set and quadratic budget.
func ValidateBallot(votes map[string]int, optionIDs []string, budget int) error {
	known := make(map[string]bool, len(optionIDs))
	for _, id := range optionIDs {
		known[id] = true
	}
	for id, v := range votes {
		if !known[id] {
			return fmt.Errorf("unknown option %q", id)
		}
		if v < 0 {
			return fmt.Errorf("votes must be non-negative")
		}
	}
	if c := BallotCost(votes); c > budget {
		return fmt.Errorf("ballot costs %d credits, budget is %d", c, budget)
	}
	return nil
}
```

- [ ] **Step 5: Run** — PASS. Commit `feat: domain types and quadratic ballot validation`.

### Task 3: Domain — scoring engine

**Files:** Create `internal/domain/scoring.go`, `internal/domain/scoring_test.go`.

- [ ] **Step 1: Failing tests** covering: simple winner; score=sum of votes across ballots; backers=count of ballots with v>0; elimination when score<threshold (threshold 1 ⇒ zero score eliminated; threshold 3 ⇒ score 2 eliminated); all-eliminated ⇒ `WinnerID==""`, `TiePending==false`; most-backers tiebreak picks fewer-points-more-backers option; most-backers still tied ⇒ deterministic with seeded rand; random tiebreak deterministic with seeded rand; creator tiebreak ⇒ `TiePending==true` with both IDs in `TiedOptionIDs`; scores sorted desc (stable by option order for equal scores); eliminated option can't win even if top score.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement**

```go
package domain

import "math/rand"

type OptionResult struct {
	OptionID   string `json:"optionId"`
	Score      int    `json:"score"`
	Backers    int    `json:"backers"`
	Eliminated bool   `json:"eliminated"`
}

type Results struct {
	Scores        []OptionResult `json:"scores"`
	WinnerID      string         `json:"winnerOptionId"`
	TiePending    bool           `json:"tiePending"`
	TiedOptionIDs []string       `json:"tiedOptionIds"`
	TiebreakNote  string         `json:"tiebreakNote"`
	RevoteCalls   int            `json:"revoteCalls"`
	RevoteNeeded  int            `json:"revoteNeeded"`
}

// ComputeResults scores ballots. optionIDs preserves creation order.
// ballots: participantID -> optionID -> votes. rnd used only for tiebreaks.
func ComputeResults(optionIDs []string, ballots map[string]map[string]int,
	survivalThreshold int, tb Tiebreaker, rnd *rand.Rand) Results {

	score := map[string]int{}
	backers := map[string]int{}
	for _, b := range ballots {
		for id, v := range b {
			if v > 0 {
				score[id] += v
				backers[id]++
			}
		}
	}
	res := Results{TiedOptionIDs: []string{}}
	for _, id := range optionIDs {
		res.Scores = append(res.Scores, OptionResult{
			OptionID: id, Score: score[id], Backers: backers[id],
			Eliminated: score[id] < survivalThreshold,
		})
	}
	// stable sort by score desc (keeps creation order within equal scores)
	sortStableByScoreDesc(res.Scores)

	// candidates: surviving options with the max surviving score
	best := -1
	for _, r := range res.Scores {
		if !r.Eliminated && r.Score > best {
			best = r.Score
		}
	}
	if best < 0 {
		return res // everything eliminated: no winner
	}
	var cands []string
	for _, r := range res.Scores {
		if !r.Eliminated && r.Score == best {
			cands = append(cands, r.OptionID)
		}
	}
	if len(cands) == 1 {
		res.WinnerID = cands[0]
		return res
	}
	switch tb {
	case TiebreakMostBackers:
		maxB := -1
		for _, id := range cands {
			if backers[id] > maxB {
				maxB = backers[id]
			}
		}
		var byBackers []string
		for _, id := range cands {
			if backers[id] == maxB {
				byBackers = append(byBackers, id)
			}
		}
		if len(byBackers) == 1 {
			res.WinnerID = byBackers[0]
			res.TiebreakNote = "tie broken by most distinct backers"
		} else {
			res.WinnerID = byBackers[rnd.Intn(len(byBackers))]
			res.TiebreakNote = "tie broken randomly"
		}
	case TiebreakRandom:
		res.WinnerID = cands[rnd.Intn(len(cands))]
		res.TiebreakNote = "tie broken randomly"
	case TiebreakCreator:
		res.TiePending = true
		res.TiedOptionIDs = cands
		res.TiebreakNote = "tied — waiting for the creator to pick"
	}
	return res
}
```

(`sortStableByScoreDesc` = `sort.SliceStable(s, func(i,j) { return s[i].Score > s[j].Score })`.)
- [ ] **Step 4: Run** — PASS. Commit `feat: quadratic scoring with veto and tiebreakers`.

### Task 4: Domain — re-vote threshold

**Files:** Create `internal/domain/revote.go`, `internal/domain/revote_test.go`.

- [ ] **Step 1: Failing tests** — `RevoteNeeded(participants, pct)`: (3,33)→1, (4,33)→2, (10,33)→4, (2,50)→1, (3,50)→2, (5,100)→5, min 1 even for (1,1). `RevoteMet(calls, participants, pct)` = calls ≥ needed.
- [ ] **Step 2:** FAIL. **Step 3: Implement**

```go
package domain

func RevoteNeeded(participants, thresholdPct int) int {
	n := (participants*thresholdPct + 99) / 100 // ceil
	if n < 1 {
		n = 1
	}
	return n
}

func RevoteMet(calls, participants, thresholdPct int) bool {
	return calls >= RevoteNeeded(participants, thresholdPct)
}
```

Check the test expectations against ceil math ((3*33+99)/100 = 1, (4*33+99)/100 = 2, (10*33+99)/100 = 4 ✓).
- [ ] **Step 4:** PASS. Commit `feat: re-vote threshold math`.

### Task 5: Store (SQLite)

**Files:** Create `internal/store/store.go`, `internal/store/store_test.go`. Deps: `modernc.org/sqlite`.

Schema (exec on open; `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`):

```sql
CREATE TABLE IF NOT EXISTS votes (
  slug TEXT PRIMARY KEY, title TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT 'suggesting',
  settings TEXT NOT NULL, creator_token TEXT NOT NULL,
  phase_deadline INTEGER, results TEXT, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  name TEXT NOT NULL, token TEXT NOT NULL UNIQUE,
  is_creator INTEGER NOT NULL DEFAULT 0,
  wants_revote INTEGER NOT NULL DEFAULT 0, joined_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS options (
  id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  participant_id TEXT NOT NULL REFERENCES participants(id),
  title TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS ballots (
  vote_slug TEXT NOT NULL REFERENCES votes(slug),
  participant_id TEXT NOT NULL REFERENCES participants(id),
  votes TEXT NOT NULL, updated_at INTEGER NOT NULL,
  PRIMARY KEY (vote_slug, participant_id));
```

Store API (all methods take a slug where relevant; a single `sync.Mutex` serializes writes; open with `sqlite.Open("file:"+path+"?_txlock=immediate")` semantics via DSN `path` and `db.SetMaxOpenConns(1)`):

```go
type Store struct{ db *sql.DB; mu sync.Mutex }
func Open(path string) (*Store, error)
func (s *Store) CreateVote(v VoteRow) error
func (s *Store) GetVote(slug string) (VoteRow, error)          // ErrNotFound
func (s *Store) UpdateVote(v VoteRow) error                    // phase, deadline, results
func (s *Store) AddParticipant(p ParticipantRow) error
func (s *Store) Participants(slug string) ([]ParticipantRow, error)
func (s *Store) ParticipantByToken(slug, token string) (ParticipantRow, error)
func (s *Store) SetWantsRevote(participantID string, want bool) error
func (s *Store) ResetRevotes(slug string) error
func (s *Store) AddOption(o OptionRow) error
func (s *Store) DeleteOption(slug, optionID, participantID string) error // only own
func (s *Store) Options(slug string) ([]OptionRow, error)      // created order
func (s *Store) PutBallot(slug, participantID, votesJSON string) error   // upsert
func (s *Store) Ballots(slug string) (map[string]map[string]int, error)  // pid→oid→v
func (s *Store) DeleteBallots(slug string) error
```

Row structs mirror columns (`Settings`/`Results` kept as JSON strings in rows; domain (de)serialization happens in the server layer).

- [ ] **Step 1: Failing tests** (use `t.TempDir()` db file): create+get vote roundtrip incl. settings JSON; ErrNotFound on missing slug; participant add/list/by-token; option add/list order/delete-own-only (deleting someone else's → no-op error); ballot upsert overwrites; Ballots() decodes map; DeleteBallots + ResetRevotes clear.
- [ ] **Step 2:** FAIL. **Step 3: Implement** (plain database/sql, `_ "modernc.org/sqlite"` driver name `sqlite`). **Step 4:** PASS. Commit `feat: sqlite store`.

### Task 6: Server core — create/join/get + snapshot builder

**Files:** Create `internal/server/server.go`, `internal/server/state.go`, `internal/server/handlers.go`, `internal/server/handlers_test.go`. Deps: chi v5.

`server.New(st *store.Store, staticFS fs.FS) *Server` (Server has chi mux, store, ws hub added in Task 8; `ServeHTTP` implements http.Handler). Static: serve `staticFS`; any non-`/api`, non-file path → `index.html` (SPA fallback).

`state.go`: `BuildRoomState(v VoteRow, parts, opts, ballots, requester *ParticipantRow) map[string]any` producing exactly the normative snapshot above. Rules: `you` null unless requester; `you.ballot` from requester's saved ballot; `results` parsed from stored JSON only when phase==results, with `revoteCalls` (count wantsRevote) and `revoteNeeded` (`domain.RevoteNeeded`) refreshed at build time; **ballot privacy:** other participants' individual ballots are never included (only aggregate scores in results).

Handlers (this task): create, join, get.
- Create: validate title non-empty (≤200 chars) and `creatorName`; merge partial settings over `DefaultSettings()` then `Validate()`; insert vote + creator participant; if `suggestTimerSecs>0` set `phase_deadline=now+secs` and register timer (Task 7 wires real scheduler; until then a no-op interface).
- Join: 404 unknown slug; name required (≤50 chars, trimmed); dedupe name with ` (2)`, ` (3)`…; any phase allowed.
- Get: Bearer token optional; invalid token → treated as spectator (`you:null`).

- [ ] **Step 1: Failing httptest tests** — create returns 201 with slug/tokens and state.phase=="suggesting", defaults applied when settings omitted, partial override (`{"creditsPerOption":5}`) keeps other defaults; create with bad settings → 400; join dupes name to `Sam (2)`; get with creator's Bearer shows `you.isCreator==true`; get without token has `you==null`; unknown slug → 404.
- [ ] **Step 2:** FAIL. **Step 3: Implement.** **Step 4:** PASS. Commit `feat: server create/join/get with room snapshots`.

### Task 7: Server — suggestions, advance, ballots, results, re-vote, timers

**Files:** Create `internal/server/advance.go`, `internal/server/timers.go`; extend `handlers.go`, `handlers_test.go`.

All mutation handlers finish by calling `s.changed(slug)` (Task 8 makes it broadcast; here it's a hook).

- Suggestions POST: phase must be suggesting (409 otherwise); cap per participant (409 "suggestion limit reached"); case-insensitive dup title → 409; then auto-check suggest-count rule → advance.
- Suggestions DELETE: suggesting phase, own option only (404/403).
- Ballot PUT: phase voting only; `domain.ValidateBallot` with budget = creditsPerOption×len(options) (400 on violation); upsert; hasVoted := ballot exists; then all-voted rule (every current participant has a ballot) → advance.
- Advance POST: creator token required (403). In results+tiePending, body `{winnerOptionId}` (must be tied — 400 otherwise) sets winner and re-stores results; else advances phase per rules (suggesting→voting requires ≥2 options → 409; voting→results computes+stores results; advancing from results without tiePending → 409 "already at results").
- Revote POST: phase results only (409); toggles caller's wantsRevote; if `RevoteMet` → `DeleteBallots`, `ResetRevotes`, results=NULL, phase=voting, deadline=voteTimer if set.
- `timers.go`: `Scheduler` with `Set(slug string, at time.Time)` / `Clear(slug)`; one goroutine per armed slug (`time.AfterFunc`), on fire calls `advancePhase(slug, byTimer)`; suggesting-with-<2-options case: clear deadline, stay in phase. On server start, re-arm deadlines found in DB (past-due fire immediately).
- Scoring rand: `rand.New(rand.NewSource(seed))` with seed from `crypto/rand`.

- [ ] **Step 1: Failing lifecycle test** (the big one, plus focused cases):
  `TestFullLifecycle`: create (2 max suggestions, creditsPerOption 3, manual advance, all-voted, threshold 1, most-backers, revote 34%) → join Bob, Carol (3 participants total) → each suggests once (Catan, Wingspan, Munchkin) → creator advances → phase voting, budget 9 → Alice ballot `{Catan:2,Wingspan:1}` (cost 5 ✓), Bob `{Wingspan:2}`, Carol votes `{Catan:1,Wingspan:1,Munchkin:0}` → after 3rd ballot phase auto-advances to results → scores: Wingspan 4 (3 backers), Catan 3 (2 backers), Munchkin 0 eliminated → winner Wingspan → Bob calls revote (1 of 2 needed, phase stays) → Carol calls revote → phase back to voting, ballots cleared, hasVoted all false.
  Focused: ballot over budget → 400; suggest in voting phase → 409; suggestion cap → 409; dup title → 409; advance without creator token → 403; advance with 1 option → 409; count-based suggest auto-advance; creator-tiebreak flow (forced tie → tiePending → advance with winnerOptionId); timer test with 1s suggest timer + 3 options → auto-advances (use short real sleep).
- [ ] **Step 2:** FAIL. **Step 3: Implement.** **Step 4:** `go test ./...` PASS. Commit `feat: full vote lifecycle — suggestions, ballots, scoring, re-vote, timers`.

### Task 8: WebSocket hub

**Files:** Create `internal/server/ws.go`; extend `handlers_test.go` (or `ws_test.go`). Dep: gorilla/websocket.

Hub: `map[slug]map[*conn]participantToken` guarded by mutex. `GET /api/votes/{slug}/ws?token=...` upgrades (token optional → spectator). On connect: send personalized snapshot. `s.changed(slug)`: for each conn of that slug, rebuild its personalized snapshot and write (single writer goroutine per conn with small buffered channel; drop conn on write error/full buffer). Ping every 30s; read loop only to detect close.

- [ ] **Step 1: Failing test** — `ws_test.go` using `httptest.NewServer` + gorilla dialer: connect two clients (Alice token, spectator), assert both get initial snapshots (`you` set vs null); POST a suggestion via REST, assert both receive an updated snapshot containing the new option within 2s.
- [ ] **Step 2:** FAIL. **Step 3: Implement**, wire `changed()` broadcast. **Step 4:** PASS. Commit `feat: websocket live room updates`.

### Task 9: Frontend scaffold + budget logic + API/WS clients

**Files:** Create under `web/`: `package.json`, `vite.config.ts` (plugin react, build.outDir `dist`, dev proxy `/api`→`http://localhost:8080` incl. ws), `tsconfig.json`, `index.html`, `src/main.tsx`, `src/types.ts`, `src/api.ts`, `src/ws.ts`, `src/budget.ts`, `src/budget.test.ts`, `src/session.ts`, `src/styles.css` (base reset + CSS custom props for light/dark via `prefers-color-scheme`).

- `types.ts`: TS mirror of the normative snapshot (`RoomState`, `Settings`, `Participant`, `Option`, `Results`, `OptionResult`) — field names exactly as in the contract.
- `budget.ts`:

```ts
export const ballotCost = (votes: Record<string, number>) =>
  Object.values(votes).reduce((s, v) => s + v * v, 0);
export const remaining = (votes: Record<string, number>, budget: number) =>
  budget - ballotCost(votes);
/** Max total votes reachable on `optionId` given the rest of the ballot. */
export const canIncrement = (
  votes: Record<string, number>, optionId: string, budget: number,
) => {
  const next = { ...votes, [optionId]: (votes[optionId] ?? 0) + 1 };
  return ballotCost(next) <= budget;
};
```

- `budget.test.ts` (Vitest): cost of {a:2,b:1}=5; remaining; canIncrement true at 0→1 with budget 1, false for 1→2 with budget 3 (cost 4), boundary exact-budget true.
- `session.ts`: `getSession(slug)` / `saveSession(slug, {sessionToken, creatorToken?, name})` over `localStorage` key `qv:<slug>`.
- `api.ts`: thin typed fetch helpers for each endpoint, throwing `Error(json.error)` on !ok.
- `ws.ts`: `connectRoom(slug, token, onState)` → WebSocket to `` `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/votes/${slug}/ws?token=…` ``; reconnect with backoff (1s, 2s, 4s… cap 15s); on reconnect the server resends the snapshot.

- [ ] **Step 1:** Scaffold configs + types + budget test (failing) → `cd web && npm install && npx vitest run` FAIL → implement `budget.ts` → PASS.
- [ ] **Step 2:** Implement api/ws/session (typecheck via `npx tsc --noEmit`).
- [ ] **Step 3:** Commit `feat: web scaffold, budget math, api/ws clients`.

### Task 10: Frontend — Home (create) page

**Files:** Create `web/src/App.tsx` (router: `/` → Home, `/v/:slug` → Room), `web/src/pages/Home.tsx`.

Home: title input + your-name input; visible defaults (max suggestions, credits per option); `<details>`-style "Advanced settings" fold: advance-rule selects (suggestion: manual / after N submitters w/ number input; voting: manual / when everyone voted), survival threshold number ("0 disables vetoes; N = minimum credits to survive"), tiebreaker select, re-vote % number, optional timers (minutes, blank = off). Submit → `POST /api/votes` → save both tokens via `session.ts` → navigate `/v/{slug}`. Client-side required-field checks; server errors shown inline.

- [ ] Implement, `npx tsc --noEmit` clean, `npm run build` clean. Commit `feat: create-vote page`.

### Task 11: Frontend — Room shell, join gate, suggesting phase

**Files:** Create `web/src/pages/Room.tsx`, `web/src/components/JoinGate.tsx`, `web/src/components/ShareLink.tsx`, `web/src/components/ParticipantList.tsx`, `web/src/components/Countdown.tsx`, `web/src/components/SuggestPhase.tsx`.

Room.tsx: load session for slug; no session → JoinGate (name → join → save session). With session: open WS, hold `RoomState`, render header (title, phase badge, Countdown when `phaseDeadline`, ShareLink copy button), ParticipantList sidebar (name + per-phase status dot: suggested/voted/wants-revote; creator crown), body by phase, creator-only Advance button with confirm + error toast (e.g. "<2 suggestions"). Countdown ticks locally from deadline timestamp; shows m:ss.

SuggestPhase: list options (title + "by Name", delete button on own), add form disabled after cap with "your N of M used" counter, dup/cap errors inline.

- [ ] Implement, typecheck + build clean. Commit `feat: room shell, join flow, suggestion phase`.

### Task 12: Frontend — voting + results phases

**Files:** Create `web/src/components/VotePhase.tsx`, `web/src/components/ResultsPhase.tsx`.

VotePhase: budget meter bar ("N of B credits used"); per option: title, − / count / + steppers (+ disabled via `canIncrement`, − at 0), cost hint "next vote costs X" (`(v+1)²−v²`); votes kept local until Submit → `PUT ballot` (resubmit allowed until phase ends, prefill from `you.ballot`); "waiting for others" note listing who hasn't voted.

ResultsPhase: winner banner (or "no winner — every option was vetoed" / tiePending: "tied — creator picks" with tied options clickable for the creator → advance with winnerOptionId); horizontal score bars scaled to max score, backers count, eliminated options greyed with "vetoed — under N credits" badge; tiebreakNote line; re-vote button toggling ("call for re-vote" / "withdraw call") with "X of Y needed"; on phase flip back to voting the WS snapshot re-renders automatically.

- [ ] Implement, typecheck + build clean. Commit `feat: voting and results phases`.

### Task 13: Polish + styles

**Files:** Extend `web/src/styles.css`; touch components only for class hookups. 404 page for unknown slug (api 404 → friendly "vote not found" screen with home link).

Design bar: clean card-based layout, max-width ~640px centered, system font stack, rounded corners, subtle borders, accent color `#6366f1`, visible focus rings, ≥44px touch targets, mobile single-column (sidebar collapses to a horizontal chip row), dark mode via `prefers-color-scheme` custom-prop swap. No CSS framework.

- [ ] Implement; `npm run build` clean; manual viewport sanity via the running app. Commit `style: responsive light/dark UI polish`.

### Task 14: Embed, Docker, README, end-to-end

**Files:** Create `webembed/embed.go`, `Dockerfile`, `docker-compose.yml`, `.dockerignore`, `README.md`, `cmd/quickvote/main.go` (final wiring).

`webembed/embed.go`:

```go
package webembed

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func FS() fs.FS {
	f, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return f
}
```

(Keep a `webembed/dist/.gitkeep`-style placeholder committed via `dist/index.html` stub so `go build` works without a web build; CI/Docker overwrite it.)

`main.go`: flags `-addr :8080`, `-db /data/quickvote.db` (env overrides `QV_ADDR`, `QV_DB`); open store, re-arm timers, serve.

Dockerfile (multi-stage): `node:22-alpine` → `COPY web/ && npm ci && npm run build`; `golang:1.22-alpine` → copy repo + built dist into `webembed/dist`, `CGO_ENABLED=0 go build -o /quickvote ./cmd/quickvote`, `go test ./...`; final `alpine:3.20` → copy binary, `VOLUME /data`, `EXPOSE 8080`, non-root user, `ENTRYPOINT ["/quickvote"]`.

docker-compose.yml: one service, `build: .`, `ports: ["8080:8080"]`, `volumes: ["quickvote-data:/data"]`.

README: what it is, quick start (`docker compose up -d` → http://localhost:8080), settings glossary (every creation option incl. veto threshold semantics), dev workflow (`go run ./cmd/quickvote` + `cd web && npm run dev`), reverse-proxy note (WS upgrade headers).

- [ ] **Steps:** write files → `go test ./...` PASS → `docker build .` succeeds → `docker compose up` → exercise a full vote through the UI (create → 2 browser profiles join → suggest → vote → results → re-vote) → commit `feat: docker packaging and docs`.

---

## Self-review notes

- Spec coverage: every settings-table row, phase rule, veto/tiebreak/re-vote behavior, API row, UI screen, and test bullet in the spec maps to Tasks 1–14. Late-joiner rules land in Task 6 (join any phase) and Task 7 (revote denominator = current participants; all-voted checks current participants).
- Type consistency: snapshot field names are single-sourced in the contract section; Tasks 3, 6, 9 reference it verbatim (`winnerOptionId`, `suggestedById`, `wantsRevote`, etc.).
- Known simplification: participants who joined during voting count toward all-voted; they must vote (or creator advances manually). Empty ballot `{}` is a valid vote (abstain) — submitting it marks hasVoted.
