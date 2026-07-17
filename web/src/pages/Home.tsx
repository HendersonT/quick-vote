import { useState, type FormEvent } from "react";
import { createVote } from "../api";
import { navigate } from "../App";
import { saveSession } from "../session";
import type { Settings, SuggestAdvanceMode, Tiebreaker, VoteAdvanceMode } from "../types";

const DEFAULTS = {
  maxSuggestionsPerUser: 3,
  creditsPerOption: 3,
  suggestAdvanceMode: "manual" as SuggestAdvanceMode,
  suggestAdvanceCount: 2,
  voteAdvanceMode: "all-voted" as VoteAdvanceMode,
  survivalThreshold: 1,
  tiebreaker: "most-backers" as Tiebreaker,
  revoteThresholdPct: 33,
};

/** Parses a "minutes, blank = off" field into seconds (0 = off). */
function minutesToSecs(raw: string): number {
  const trimmed = raw.trim();
  if (trimmed === "") return 0;
  const minutes = Number(trimmed);
  if (!Number.isFinite(minutes) || minutes <= 0) return 0;
  return Math.round(minutes * 60);
}

export default function Home() {
  const [title, setTitle] = useState("");
  const [creatorName, setCreatorName] = useState("");

  const [maxSuggestionsPerUser, setMaxSuggestionsPerUser] = useState(
    DEFAULTS.maxSuggestionsPerUser,
  );
  const [creditsPerOption, setCreditsPerOption] = useState(DEFAULTS.creditsPerOption);

  const [suggestAdvanceMode, setSuggestAdvanceMode] = useState<SuggestAdvanceMode>(
    DEFAULTS.suggestAdvanceMode,
  );
  const [suggestAdvanceCount, setSuggestAdvanceCount] = useState(
    DEFAULTS.suggestAdvanceCount,
  );
  const [voteAdvanceMode, setVoteAdvanceMode] = useState<VoteAdvanceMode>(
    DEFAULTS.voteAdvanceMode,
  );
  const [survivalThreshold, setSurvivalThreshold] = useState(DEFAULTS.survivalThreshold);
  const [tiebreaker, setTiebreaker] = useState<Tiebreaker>(DEFAULTS.tiebreaker);
  const [revoteThresholdPct, setRevoteThresholdPct] = useState(
    DEFAULTS.revoteThresholdPct,
  );
  const [suggestTimerMinutes, setSuggestTimerMinutes] = useState("");
  const [voteTimerMinutes, setVoteTimerMinutes] = useState("");

  const [fieldError, setFieldError] = useState<string | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setServerError(null);

    const trimmedTitle = title.trim();
    const trimmedName = creatorName.trim();

    if (!trimmedTitle) {
      setFieldError("Give the vote a title.");
      return;
    }
    if (trimmedTitle.length > 200) {
      setFieldError("Title must be 200 characters or fewer.");
      return;
    }
    if (!trimmedName) {
      setFieldError("Enter your name.");
      return;
    }
    if (trimmedName.length > 50) {
      setFieldError("Name must be 50 characters or fewer.");
      return;
    }
    setFieldError(null);

    const settings: Partial<Settings> = {
      maxSuggestionsPerUser,
      creditsPerOption,
      suggestAdvanceMode,
      suggestAdvanceCount: suggestAdvanceMode === "count" ? suggestAdvanceCount : 0,
      voteAdvanceMode,
      survivalThreshold,
      tiebreaker,
      revoteThresholdPct,
      suggestTimerSecs: minutesToSecs(suggestTimerMinutes),
      voteTimerSecs: minutesToSecs(voteTimerMinutes),
    };

    setSubmitting(true);
    try {
      const { slug, creatorToken, sessionToken } = await createVote(
        trimmedTitle,
        trimmedName,
        settings,
      );
      saveSession(slug, { sessionToken, creatorToken, name: trimmedName });
      navigate(`/v/${slug}`);
    } catch (err) {
      setServerError(err instanceof Error ? err.message : "Failed to create vote.");
      setSubmitting(false);
    }
  }

  return (
    <main className="home-page">
      <h1>Quick Vote</h1>
      <p className="home-tagline">
        Suggest options, vote quadratically, land on a winner.
      </p>

      <form className="home-form" onSubmit={handleSubmit}>
        {(fieldError || serverError) && (
          <p className="error-banner" role="alert">
            {fieldError ?? serverError}
          </p>
        )}

        <div className="field">
          <label htmlFor="title">Vote title</label>
          <input
            id="title"
            type="text"
            value={title}
            maxLength={200}
            placeholder="Friday game night"
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </div>

        <div className="field">
          <label htmlFor="creatorName">Your name</label>
          <input
            id="creatorName"
            type="text"
            value={creatorName}
            maxLength={50}
            placeholder="Sam"
            onChange={(e) => setCreatorName(e.target.value)}
            required
          />
        </div>

        <div className="field-row">
          <div className="field">
            <label htmlFor="maxSuggestionsPerUser">Max suggestions per person</label>
            <input
              id="maxSuggestionsPerUser"
              type="number"
              min={1}
              max={20}
              value={maxSuggestionsPerUser}
              onChange={(e) => setMaxSuggestionsPerUser(Number(e.target.value))}
            />
          </div>

          <div className="field">
            <label htmlFor="creditsPerOption">Credits per option</label>
            <input
              id="creditsPerOption"
              type="number"
              min={1}
              max={100}
              value={creditsPerOption}
              onChange={(e) => setCreditsPerOption(Number(e.target.value))}
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
              value={suggestAdvanceMode}
              onChange={(e) =>
                setSuggestAdvanceMode(e.target.value as SuggestAdvanceMode)
              }
            >
              <option value="manual">Manual (creator advances)</option>
              <option value="count">After N people have suggested</option>
            </select>
            {suggestAdvanceMode === "count" && (
              <div className="field">
                <label htmlFor="suggestAdvanceCount">
                  Number of submitters needed
                </label>
                <input
                  id="suggestAdvanceCount"
                  type="number"
                  min={1}
                  value={suggestAdvanceCount}
                  onChange={(e) => setSuggestAdvanceCount(Number(e.target.value))}
                />
              </div>
            )}
          </div>

          <div className="field">
            <label htmlFor="voteAdvanceMode">Voting-phase advance</label>
            <select
              id="voteAdvanceMode"
              value={voteAdvanceMode}
              onChange={(e) => setVoteAdvanceMode(e.target.value as VoteAdvanceMode)}
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
              value={survivalThreshold}
              onChange={(e) => setSurvivalThreshold(Number(e.target.value))}
            />
            <p className="field-hint">
              0 disables vetoes; N = minimum credits an option needs to survive.
            </p>
          </div>

          <div className="field">
            <label htmlFor="tiebreaker">Tiebreaker</label>
            <select
              id="tiebreaker"
              value={tiebreaker}
              onChange={(e) => setTiebreaker(e.target.value as Tiebreaker)}
            >
              <option value="most-backers">Most distinct backers, then random</option>
              <option value="random">Random</option>
              <option value="creator">Creator picks</option>
            </select>
          </div>

          <div className="field">
            <label htmlFor="revoteThresholdPct">Re-vote threshold (%)</label>
            <input
              id="revoteThresholdPct"
              type="number"
              min={1}
              max={100}
              value={revoteThresholdPct}
              onChange={(e) => setRevoteThresholdPct(Number(e.target.value))}
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
                value={suggestTimerMinutes}
                onChange={(e) => setSuggestTimerMinutes(e.target.value)}
              />
            </div>

            <div className="field">
              <label htmlFor="voteTimerMinutes">Voting timer (minutes)</label>
              <input
                id="voteTimerMinutes"
                type="number"
                min={0}
                placeholder="off"
                value={voteTimerMinutes}
                onChange={(e) => setVoteTimerMinutes(e.target.value)}
              />
            </div>
          </div>
        </details>

        <div className="actions">
          <button type="submit" disabled={submitting}>
            {submitting ? "Creating…" : "Create vote"}
          </button>
        </div>
      </form>
    </main>
  );
}
