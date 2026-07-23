// TS mirror of the normative room-state snapshot. Field names match the
// Go contract exactly (see docs/superpowers/plans/2026-07-17-quick-vote.md).

export type Phase = "suggesting" | "voting" | "results";
export type SuggestAdvanceMode = "manual" | "count" | "suggestion-count" | "all-done";
export type VoteAdvanceMode = "manual" | "all-voted";
export type Tiebreaker = "most-backers" | "random" | "creator" | "earliest" | "runoff";
// RunoffFallback is Tiebreaker minus "runoff" itself — a runoff round that
// ties again must resolve, never re-enter another runoff.
export type RunoffFallback = "most-backers" | "random" | "creator" | "earliest";

export interface Settings {
  maxSuggestionsPerUser: number;
  creditsPerOption: number;
  suggestAdvanceMode: SuggestAdvanceMode;
  suggestAdvanceCount: number;
  voteAdvanceMode: VoteAdvanceMode;
  survivalThreshold: number;
  tiebreaker: Tiebreaker;
  revoteThresholdPct: number;
  suggestTimerSecs: number;
  voteTimerSecs: number;
  // vetoCost is the credit price of vetoing an option (ballot value -1).
  // 0 disables veto entirely (default; preserves current behavior).
  vetoCost: number;
  // voteScalingExponent controls how steeply the cost of stacking credits
  // on one option grows: cost(v) = ceil(v^exponent - epsilon). 2.0 (the
  // default) reproduces the original quadratic cost.
  voteScalingExponent: number;
  // runoffFallback is the tiebreaker used to resolve a runoff round that
  // ties again.
  runoffFallback: RunoffFallback;
}

export interface Participant {
  id: string;
  name: string;
  isCreator: boolean;
  hasSuggested: boolean;
  hasVoted: boolean;
  wantsRevote: boolean;
  doneSuggesting: boolean;
}

export interface Option {
  id: string;
  title: string;
  suggestedById: string;
  // active is false when a "runoff" tiebreaker round has restricted voting
  // to only the tied options (see RoomState.runoff). Always true otherwise.
  active: boolean;
}

export interface You {
  participantId: string;
  isCreator: boolean;
  ballot: Record<string, number> | null;
}

export interface OptionResult {
  optionId: string;
  score: number;
  backers: number;
  // vetoCount is the number of participants who explicitly vetoed this
  // option (ballot value -1). vetoCount > 0 eliminates it regardless of
  // score.
  vetoCount: number;
  eliminated: boolean;
}

export interface Results {
  scores: OptionResult[];
  winnerOptionId: string;
  tiePending: boolean;
  tiedOptionIds: string[];
  tiebreakNote: string;
  revoteCalls: number;
  revoteNeeded: number;
  // runoffPending is true when tiebreaker "runoff" hit a fresh tie; the
  // server transitions straight back to voting in that case, so this never
  // actually surfaces in a `results` snapshot — present for contract parity.
  runoffPending: boolean;
}

export interface RoomState {
  slug: string;
  title: string;
  phase: Phase;
  phaseDeadline: string | null;
  settings: Settings;
  participants: Participant[];
  options: Option[];
  budget: number;
  you: You | null;
  results: Results | null;
  // runoff is true iff a "runoff" tiebreaker round is currently restricting
  // voting to a subset of options (see Option.active).
  runoff: boolean;
}
