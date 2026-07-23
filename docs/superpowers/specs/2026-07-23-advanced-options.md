# Quick-Vote: Advanced Options Round 2 — Spec

Date: 2026-07-23. This spec is authoritative for this round of changes. Defaults
must preserve current behavior exactly; every new behavior is opt-in.

## Features

### F1. "I'm done suggesting" flag
- Participants can mark themselves done during the suggesting phase, regardless
  of how many suggestions they made (including zero).
- Schema: `participants.done_suggesting INTEGER NOT NULL DEFAULT 0` (additive
  migration, see Migrations).
- API: `POST /api/votes/{slug}/done-suggesting` — toggles the caller's flag.
  409 unless phase == suggesting. Requires session token (requireParticipant).
  Returns the fresh room state like other mutating handlers. Broadcasts via
  `s.changed(slug)`. Must take the per-slug lock (`defer s.lockSlug(slug)()`).
- Room state: each participant object gains `"doneSuggesting": bool`.
- Marking done does NOT block adding/deleting suggestions afterwards, and
  adding a suggestion does NOT clear the flag. It is purely an explicit toggle.
- UI: button in SuggestPhase ("I'm done suggesting" / "Resume suggesting"),
  and ParticipantList shows the done state during the suggesting phase
  (a participant is "done" in suggesting phase iff doneSuggesting).

### F2. Suggest-advance modes (incl. "After N suggestions")
`suggestAdvanceMode` gains two new values (existing "manual" and "count" keep
their exact current semantics — "count" = N distinct participants have
suggested):
- `"suggestion-count"` — auto-advance when the TOTAL number of suggestions
  >= suggestAdvanceCount.
- `"all-done"` — auto-advance when EVERY current participant has
  doneSuggesting == true.
All auto-advance paths still require >= 2 options total (existing rule).
`maybeAutoAdvanceSuggest` must be re-checked on: suggestion create, suggestion
delete, and done-suggesting toggle. Validation: when mode is "count" or
"suggestion-count", suggestAdvanceCount must be 1..1000.

### F3. Explicit vetoes that cost credits (advanced option)
- New setting `vetoCost int` (json `vetoCost`), default 0 = feature disabled.
  Range 0..100.
- When vetoCost > 0, a voter may explicitly veto options on their ballot.
  Encoding: in the ballot votes map, a value of **-1 means veto**. Values >= 0
  remain credit allocations. -1 is rejected with a 400 when vetoCost == 0;
  any value < -1 is always rejected. You cannot both vote for and veto the
  same option (trivially true with this encoding).
- Each veto costs `vetoCost` credits from the voter's normal quadratic budget:
  ballot cost = sum over v>0 of cost(v) + vetoCost * (number of -1 entries).
- Scoring: an option explicitly vetoed by ANY participant is eliminated.
  `OptionResult` gains `"vetoCount": int` (number of participants who vetoed
  it). Eliminated = vetoCount > 0 || score < survivalThreshold. Vetoed options
  never win. Positive votes from other voters still display as score.
- UI (VotePhase): when vetoCost > 0, each option row gets a veto toggle
  (e.g. a ⛔ button showing "veto (−{vetoCost}c)"). Vetoing an option zeroes
  any credits you had on it. Remaining-credit math includes veto costs.
  ResultsPhase: vetoed options show "vetoed by N voter(s)" badge, distinct
  from the existing under-threshold badge.

### F4. Adjustable vote-cost scaling (advanced option)
- New setting `voteScalingExponent float64` (json `voteScalingExponent`),
  default 2.0, valid range 1.0..4.0. Cost of putting v (>0) votes on one
  option = `ceil(pow(v, exponent) - 1e-9)` (the epsilon keeps exact powers
  like 3^2=9 from rounding up on float error). Exponent 2.0 reproduces v*v.
- Go and TypeScript MUST use the same formula (Go: math.Ceil(math.Pow(v,e)-1e-9),
  TS: Math.ceil(Math.pow(v,e)-1e-9)).
- Keep the existing per-option overflow guard: reject any single option's
  v > budget (valid since cost(v) >= v for exponent >= 1).
- **Legacy-decode normalization**: settings JSON stored on old votes lacks the
  new fields, so add `func (s Settings) Normalized() Settings` that maps
  voteScalingExponent == 0 -> 2.0, empty RunoffFallback -> "random", and apply
  it at EVERY decode site (parseSettings, BuildRoomState, advancePhase).
  DefaultSettings() sets the new fields explicitly.
- UI: advanced option — number input or select (1 = linear, 2 = quadratic
  default, up to 4), step 0.5. Budget helper text in VotePhase reflects it.

### F5. Advanced tiebreakers
`tiebreaker` gains two new values (existing three unchanged):
- `"earliest"` — the tied option suggested earliest wins.
  Note: `ComputeResults`'s optionIDs input is creation-ordered and the
  stable sort preserves that order among equal scores, so the first collected
  candidate is the earliest. TiebreakNote: "tie broken by earliest suggestion".
- `"runoff"` — a tie triggers an automatic runoff: the vote returns to the
  voting phase with ONLY the tied options active, ballots cleared, vote timer
  re-armed if configured.
- New setting `runoffFallback` (json `runoffFallback`), default "random",
  valid values: most-backers | random | creator | earliest (NOT runoff — a
  runoff round that ties again resolves with the fallback, guaranteeing
  termination after one runoff round).

Runoff mechanics:
- Schema: `votes.active_options TEXT` (JSON array of option IDs; NULL = all
  options active). Additive migration.
- `VoteRow` gains `ActiveOptions *string`; CreateVote/GetVote/UpdateVote carry
  it (UpdateVote updates it).
- Everywhere "the options" are used for voting/scoring/budget (handlePutBallot
  validation, budget = creditsPerOption * len(activeOptions), enterResults,
  BuildRoomState's budget), restrict to active options when active_options is
  set. Room state: each option object gains `"active": bool` (all true when
  no runoff), and top-level `"runoff": bool` (true iff active_options != NULL).
- ComputeResults gets the runoff context: signature grows to take a tiebreak
  config struct (tiebreaker, runoffFallback, inRunoff bool). If tiebreaker ==
  runoff and !inRunoff and there is a multi-way tie: return Results with a new
  field `"runoffPending": true` and TiedOptionIDs set. The server
  (enterResults) then, instead of entering results: sets active_options to the
  tied IDs, deletes all ballots, sets phase = voting, arms VoteTimerSecs if
  configured, and broadcasts. If inRunoff (or fallback needed), resolve the
  tie with runoffFallback's semantics (creator fallback => TiePending flow as
  today).
- Transitions that reset active_options to NULL (all active again):
  suggesting -> voting, and a threshold re-vote (handleRevote).
- UI: VotePhase shows only active options with a banner during runoff
  ("Runoff vote — tied options only"); Home advanced settings: tiebreaker
  select gains the two options, plus a fallback select shown when runoff is
  chosen. ResultsPhase can rely on tiebreakNote ("tie broken after runoff by
  ..." — set a distinguishable note when resolved in a runoff round).

### F6. Recent votes list (results retention / navigation)
- No backend change: votes/results are already persisted indefinitely.
- web/src/session.ts gains a client-side history: key `qv:history`, a JSON
  array of `{slug, title, ts}` (most recent first, deduped by slug, capped at
  20). `addToHistory(slug, title)` called after successful create (Home) and
  successful join/room load (Room). `getHistory()` returns the list.
- Home page renders a "Recent votes" section (when non-empty) linking each
  entry to `/{slug}` with its title and relative/absolute date.
- README: document that data is kept indefinitely in SQLite and rooms can be
  revisited by URL / recent list.

## Migrations
`store.Open` applies the base schema (CREATE TABLE IF NOT EXISTS — will NOT
add columns to existing tables), then additive migrations: for each of
`participants.done_suggesting` and `votes.active_options`, check
`PRAGMA table_info(<table>)` and `ALTER TABLE ... ADD COLUMN` when missing.
New columns must also be added to the base schema string for fresh DBs.

## Settings summary (new/changed JSON fields)
| field | type | default | range/values |
|---|---|---|---|
| vetoCost | int | 0 (off) | 0..100 |
| voteScalingExponent | float | 2.0 | 1.0..4.0 |
| runoffFallback | string | "random" | most-backers, random, creator, earliest |
| suggestAdvanceMode | string | "manual" | manual, count, suggestion-count, all-done |
| tiebreaker | string | most-backers | + earliest, runoff |

settingsPatch (create-time overrides) gains pointer fields for the three new
settings. Validate() covers all new ranges/enums and rejects
runoffFallback == "runoff".

## Testing requirements
- Go: unit tests for new BallotCost/ValidateBallot (exponent + veto paths,
  including -1 with vetoCost 0, cost accounting, epsilon behavior for
  exponent 1.5), ComputeResults (vetoCount elimination, earliest tiebreak,
  runoffPending, runoff fallback incl. creator), settings Validate/Normalized,
  store migrations (open an existing old-schema DB and verify columns added),
  handler tests for done-suggesting toggle + all-done and suggestion-count
  advance, veto ballot end-to-end, runoff end-to-end (tie -> runoff voting ->
  ballots cleared -> only active options votable -> second results).
- TS: budget.test.ts extended for exponent + veto costs.
- `go test ./...`, `go vet ./...`, `cd web && npm test && npm run build` must
  all pass. After frontend changes build cleanly, refresh the embedded SPA:
  `rm -rf webembed/dist && cp -r web/dist webembed/dist`.

## Non-goals
- No data retention/cleanup policy changes.
- No change to existing default behaviors, WS protocol shape beyond the added
  fields, or the re-vote threshold feature.
