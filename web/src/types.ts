// TS mirror of the normative room-state snapshot. Field names match the
// Go contract exactly (see docs/superpowers/plans/2026-07-17-quick-vote.md).

export type Phase = "suggesting" | "voting" | "results";
export type SuggestAdvanceMode = "manual" | "count";
export type VoteAdvanceMode = "manual" | "all-voted";
export type Tiebreaker = "most-backers" | "random" | "creator";

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
}

export interface Participant {
  id: string;
  name: string;
  isCreator: boolean;
  hasSuggested: boolean;
  hasVoted: boolean;
  wantsRevote: boolean;
}

export interface Option {
  id: string;
  title: string;
  suggestedById: string;
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
}
