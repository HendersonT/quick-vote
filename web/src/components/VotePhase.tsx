import { useEffect, useState } from "react";
import { putBallot } from "../api";
import { ballotCost, canIncrement, remaining } from "../budget";
import type { RoomState } from "../types";

interface VotePhaseProps {
  slug: string;
  sessionToken: string;
  state: RoomState;
}

/** Ballot with per-option steppers, a live budget meter, and submit. */
export default function VotePhase({ slug, sessionToken, state }: VotePhaseProps) {
  const [votes, setVotes] = useState<Record<string, number>>(() => state.you?.ballot ?? {});
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  const budget = state.budget;

  // Prefill (or re-prefill on reconnect) from the saved ballot, but don't
  // clobber in-progress local edits once the participant has voted.
  useEffect(() => {
    setVotes(state.you?.ballot ?? {});
  }, [state.you?.ballot]);

  const spent = ballotCost(votes);
  const left = remaining(votes, budget);

  function increment(optionId: string) {
    if (!canIncrement(votes, optionId, budget)) return;
    setVotes((v) => ({ ...v, [optionId]: (v[optionId] ?? 0) + 1 }));
  }

  function decrement(optionId: string) {
    setVotes((v) => {
      const current = v[optionId] ?? 0;
      if (current <= 0) return v;
      return { ...v, [optionId]: current - 1 };
    });
  }

  function nextCost(optionId: string): number {
    const v = votes[optionId] ?? 0;
    return (v + 1) * (v + 1) - v * v;
  }

  async function handleSubmit() {
    setError(null);
    setSubmitting(true);
    try {
      await putBallot(slug, sessionToken, votes);
      setSubmitted(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to submit ballot.");
    } finally {
      setSubmitting(false);
    }
  }

  const waitingOn = state.participants.filter((p) => !p.hasVoted);

  return (
    <section className="vote-phase">
      <h2>Vote</h2>
      <div className={`budget-meter ${left < 0 ? "over-budget" : ""}`} role="status">
        <div className="budget-meter-bar">
          <div
            className="budget-meter-fill"
            style={{ width: `${budget > 0 ? Math.min(100, (spent / budget) * 100) : 0}%` }}
          />
        </div>
        <span className="budget-meter-label">
          {spent} of {budget} credits used
        </span>
      </div>

      {error && (
        <p className="error-banner" role="alert">
          {error}
        </p>
      )}

      <ul className="ballot-list">
        {state.options.map((o) => {
          const count = votes[o.id] ?? 0;
          const canPlus = canIncrement(votes, o.id, budget);
          return (
            <li key={o.id} className="ballot-item">
              <span className="option-title">{o.title}</span>
              <div className="stepper">
                <button
                  type="button"
                  className="stepper-button"
                  onClick={() => decrement(o.id)}
                  disabled={count <= 0}
                  aria-label={`Decrease votes for ${o.title}`}
                >
                  −
                </button>
                <span className="stepper-count">{count}</span>
                <button
                  type="button"
                  className="stepper-button"
                  onClick={() => increment(o.id)}
                  disabled={!canPlus}
                  aria-label={`Increase votes for ${o.title}`}
                >
                  +
                </button>
              </div>
              <span className="cost-hint">next vote costs {nextCost(o.id)}</span>
            </li>
          );
        })}
      </ul>

      <div className="actions">
        <button type="button" onClick={handleSubmit} disabled={submitting || left < 0}>
          {submitting ? "Submitting…" : submitted ? "Resubmit ballot" : "Submit ballot"}
        </button>
        {left < 0 && <span className="error-banner">over budget by {-left}</span>}
      </div>

      {waitingOn.length > 0 && (
        <p className="waiting-note">
          Waiting on: {waitingOn.map((p) => p.name).join(", ")}
        </p>
      )}
    </section>
  );
}
