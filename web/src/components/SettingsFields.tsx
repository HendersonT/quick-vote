import type {
  RunoffFallback,
  Settings,
  SuggestAdvanceMode,
  Tiebreaker,
  VoteAdvanceMode,
} from "../types";

/**
 * Form state for a vote's settings. Timers are kept as the raw "minutes,
 * blank = off" input strings so the field can be cleared while typing.
 */
export interface SettingsFormState {
  maxSuggestionsPerUser: number;
  creditsPerOption: number;
  suggestAdvanceMode: SuggestAdvanceMode;
  suggestAdvanceCount: number;
  voteAdvanceMode: VoteAdvanceMode;
  survivalThreshold: number;
  tiebreaker: Tiebreaker;
  runoffFallback: RunoffFallback;
  revoteThresholdPct: number;
  suggestTimerMinutes: string;
  voteTimerMinutes: string;
  vetoCost: number;
  voteScalingExponent: number;
}

export const DEFAULT_SETTINGS_FORM: SettingsFormState = {
  maxSuggestionsPerUser: 3,
  creditsPerOption: 3,
  suggestAdvanceMode: "manual",
  suggestAdvanceCount: 2,
  voteAdvanceMode: "all-voted",
  survivalThreshold: 0,
  tiebreaker: "most-backers",
  runoffFallback: "random",
  revoteThresholdPct: 33,
  suggestTimerMinutes: "",
  voteTimerMinutes: "",
  vetoCost: 0,
  voteScalingExponent: 2,
};

function needsCount(mode: SuggestAdvanceMode): boolean {
  return mode === "count" || mode === "suggestion-count";
}

/** Parses a "minutes, blank = off" field into seconds (0 = off). */
function minutesToSecs(raw: string): number {
  const trimmed = raw.trim();
  if (trimmed === "") return 0;
  const minutes = Number(trimmed);
  if (!Number.isFinite(minutes) || minutes <= 0) return 0;
  return Math.round(minutes * 60);
}

/** Inverse of minutesToSecs for pre-filling from an existing vote. */
function secsToMinutes(secs: number): string {
  return secs > 0 ? String(Math.round((secs / 60) * 100) / 100) : "";
}

/** Converts form state into the settings payload for create / next vote. */
export function toSettings(form: SettingsFormState): Partial<Settings> {
  return {
    maxSuggestionsPerUser: form.maxSuggestionsPerUser,
    creditsPerOption: form.creditsPerOption,
    suggestAdvanceMode: form.suggestAdvanceMode,
    suggestAdvanceCount: needsCount(form.suggestAdvanceMode) ? form.suggestAdvanceCount : 0,
    voteAdvanceMode: form.voteAdvanceMode,
    survivalThreshold: form.survivalThreshold,
    tiebreaker: form.tiebreaker,
    runoffFallback: form.runoffFallback,
    revoteThresholdPct: form.revoteThresholdPct,
    suggestTimerSecs: minutesToSecs(form.suggestTimerMinutes),
    voteTimerSecs: minutesToSecs(form.voteTimerMinutes),
    vetoCost: form.vetoCost,
    voteScalingExponent: form.voteScalingExponent,
  };
}

/** Seeds form state from an existing vote's normalized settings. */
export function fromSettings(s: Settings): SettingsFormState {
  return {
    maxSuggestionsPerUser: s.maxSuggestionsPerUser,
    creditsPerOption: s.creditsPerOption,
    suggestAdvanceMode: s.suggestAdvanceMode,
    suggestAdvanceCount: needsCount(s.suggestAdvanceMode)
      ? s.suggestAdvanceCount
      : DEFAULT_SETTINGS_FORM.suggestAdvanceCount,
    voteAdvanceMode: s.voteAdvanceMode,
    survivalThreshold: s.survivalThreshold,
    tiebreaker: s.tiebreaker,
    runoffFallback: s.runoffFallback,
    revoteThresholdPct: s.revoteThresholdPct,
    suggestTimerMinutes: secsToMinutes(s.suggestTimerSecs),
    voteTimerMinutes: secsToMinutes(s.voteTimerSecs),
    vetoCost: s.vetoCost,
    voteScalingExponent: s.voteScalingExponent,
  };
}

interface SettingsFieldsProps {
  value: SettingsFormState;
  onChange: (next: SettingsFormState) => void;
}

/**
 * The vote-settings inputs (basic fields plus the collapsible advanced
 * section), shared by the create form on Home and the "start another vote
 * with this group" form in a room.
 */
export default function SettingsFields({ value, onChange }: SettingsFieldsProps) {
  function set<K extends keyof SettingsFormState>(key: K, v: SettingsFormState[K]) {
    onChange({ ...value, [key]: v });
  }
  const needsSuggestAdvanceCount = needsCount(value.suggestAdvanceMode);

  return (
    <>
      <div className="field-row">
        <div className="field">
          <label htmlFor="maxSuggestionsPerUser">Max suggestions per person</label>
          <input
            id="maxSuggestionsPerUser"
            type="number"
            min={1}
            max={20}
            value={value.maxSuggestionsPerUser}
            onChange={(e) => set("maxSuggestionsPerUser", Number(e.target.value))}
          />
        </div>

        <div className="field">
          <label htmlFor="creditsPerOption">Credits per option</label>
          <input
            id="creditsPerOption"
            type="number"
            min={1}
            max={100}
            value={value.creditsPerOption}
            onChange={(e) => set("creditsPerOption", Number(e.target.value))}
          />
          <p className="field-hint">
            Total voting budget = credits per option × number of options.
          </p>
        </div>
      </div>

      <details className="advanced-settings">
        <summary>Advanced settings</summary>

        <div className="field">
          <label htmlFor="suggestAdvanceMode">Suggestion-phase advance</label>
          <select
            id="suggestAdvanceMode"
            value={value.suggestAdvanceMode}
            onChange={(e) =>
              set("suggestAdvanceMode", e.target.value as SuggestAdvanceMode)
            }
          >
            <option value="manual">Manual (creator advances)</option>
            <option value="count">After N people have suggested</option>
            <option value="suggestion-count">After N total suggestions</option>
            <option value="all-done">When everyone marks done</option>
          </select>
          {needsSuggestAdvanceCount && (
            <div className="field">
              <label htmlFor="suggestAdvanceCount">
                {value.suggestAdvanceMode === "count"
                  ? "Number of submitters needed"
                  : "Number of suggestions needed"}
              </label>
              <input
                id="suggestAdvanceCount"
                type="number"
                min={1}
                max={1000}
                value={value.suggestAdvanceCount}
                onChange={(e) => set("suggestAdvanceCount", Number(e.target.value))}
              />
            </div>
          )}
        </div>

        <div className="field">
          <label htmlFor="voteAdvanceMode">Voting-phase advance</label>
          <select
            id="voteAdvanceMode"
            value={value.voteAdvanceMode}
            onChange={(e) => set("voteAdvanceMode", e.target.value as VoteAdvanceMode)}
          >
            <option value="manual">Manual (creator advances)</option>
            <option value="all-voted">When everyone has voted</option>
          </select>
        </div>

        <div className="field">
          <label htmlFor="survivalThreshold">Survival threshold</label>
          <input
            id="survivalThreshold"
            type="number"
            min={0}
            value={value.survivalThreshold}
            onChange={(e) => set("survivalThreshold", Number(e.target.value))}
          />
          <p className="field-hint">
            0 disables the threshold; N = minimum total votes an option needs
            to survive.
          </p>
        </div>

        <div className="field">
          <label htmlFor="vetoCost">Veto cost</label>
          <input
            id="vetoCost"
            type="number"
            min={0}
            max={100}
            value={value.vetoCost}
            onChange={(e) => set("vetoCost", Number(e.target.value))}
          />
          <p className="field-hint">
            0 disables vetoes; N = credits it costs a voter to explicitly veto
            an option (a vetoed option is eliminated no matter its score).
          </p>
        </div>

        <div className="field">
          <label htmlFor="voteScalingExponent">Vote cost scaling</label>
          <input
            id="voteScalingExponent"
            type="number"
            min={1}
            max={4}
            step={0.5}
            value={value.voteScalingExponent}
            onChange={(e) => set("voteScalingExponent", Number(e.target.value))}
          />
          <p className="field-hint">
            Cost of stacking v votes on one option = v raised to this power
            (1 = linear, 2 = quadratic default, higher = steeper penalty for
            piling on).
          </p>
        </div>

        <div className="field">
          <label htmlFor="tiebreaker">Tiebreaker</label>
          <select
            id="tiebreaker"
            value={value.tiebreaker}
            onChange={(e) => set("tiebreaker", e.target.value as Tiebreaker)}
          >
            <option value="most-backers">Most distinct backers, then random</option>
            <option value="random">Random</option>
            <option value="creator">Creator picks</option>
            <option value="earliest">Earliest suggestion wins</option>
            <option value="runoff">Runoff re-vote among tied options</option>
          </select>
          {value.tiebreaker === "runoff" && (
            <div className="field">
              <label htmlFor="runoffFallback">
                Fallback if the runoff ties again
              </label>
              <select
                id="runoffFallback"
                value={value.runoffFallback}
                onChange={(e) => set("runoffFallback", e.target.value as RunoffFallback)}
              >
                <option value="most-backers">Most distinct backers, then random</option>
                <option value="random">Random</option>
                <option value="creator">Creator picks</option>
                <option value="earliest">Earliest suggestion wins</option>
              </select>
            </div>
          )}
        </div>

        <div className="field">
          <label htmlFor="revoteThresholdPct">Re-vote threshold (%)</label>
          <input
            id="revoteThresholdPct"
            type="number"
            min={1}
            max={100}
            value={value.revoteThresholdPct}
            onChange={(e) => set("revoteThresholdPct", Number(e.target.value))}
          />
        </div>

        <div className="field-row">
          <div className="field">
            <label htmlFor="suggestTimerMinutes">Suggestion timer (minutes)</label>
            <input
              id="suggestTimerMinutes"
              type="number"
              min={0}
              placeholder="off"
              value={value.suggestTimerMinutes}
              onChange={(e) => set("suggestTimerMinutes", e.target.value)}
            />
          </div>

          <div className="field">
            <label htmlFor="voteTimerMinutes">Voting timer (minutes)</label>
            <input
              id="voteTimerMinutes"
              type="number"
              min={0}
              placeholder="off"
              value={value.voteTimerMinutes}
              onChange={(e) => set("voteTimerMinutes", e.target.value)}
            />
          </div>
        </div>
      </details>
    </>
  );
}
