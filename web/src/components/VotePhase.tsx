import { useEffect, useState } from "react";
import { putBallot } from "../api";
import { ballotCost, canIncrement, remaining, voteCost } from "../budget";
import type { RoomState } from "../types";

interface VotePhaseProps {
  slug: string;
  sessionToken: string;
  state: RoomState;
  /** True while the vote is closed: the ballot is read-only (spec B3). */
  closed?: boolean;
}

/** Human-readable description of the vote's cost-scaling exponent. */
function scalingHint(exponent: number): string {
  if (exponent === 1) return "1 = linear: cost equals votes cast.";
  if (exponent === 2) return "2 = quadratic: cost is votes squared.";
  return `${exponent} = cost grows as votes^${exponent}.`;
}

/** Ballot with per-option steppers, veto toggles, a live budget meter, and submit. */
export default function VotePhase({
  slug,
  sessionToken,
  state,
  closed = false,
}: VotePhaseProps) {
  const [votes, setVotes] = useState<Record<string, number>>(() => state.you?.ballot ?? {});
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  const budget = state.budget;
  const exponent = state.settings.voteScalingExponent;
  const vetoCost = state.settings.vetoCost;
  const vetoEnabled = vetoCost > 0;
  // Only active options are votable — outside a runoff round every option is
  // active, so this is a no-op in the common case (F5).
  const votableOptions = state.options.filter((o) => o.active);

  // Prefill (or re-prefill on reconnect) from the saved ballot. Every WS
  // snapshot arrives as a freshly-parsed object, so keying this effect on the
  // ballot reference would re-run on every unrelated broadcast (e.g. another
  // participant voting) and wipe out the user's in-progress steppers. Keying on
  // the serialized content means we only re-seed when the *saved* ballot
  // actually changes (initial load, reconnect, or a resubmit), never on an
  // incidental broadcast that leaves this participant's ballot untouched.
  const savedBallotKey = JSON.stringify(state.you?.ballot ?? {});
  useEffect(() => {
    setVotes(state.you?.ballot ?? {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [savedBallotKey]);

  const spent = ballotCost(votes, exponent, vetoCost);
  const left = remaining(votes, budget, exponent, vetoCost);

  function increment(optionId: string) {
    if (!canIncrement(votes, optionId, budget, exponent, vetoCost)) return;
    setVotes((v) => ({ ...v, [optionId]: Math.max(0, v[optionId] ?? 0) + 1 }));
  }

  function decrement(optionId: string) {
    setVotes((v) => {
      const current = v[optionId] ?? 0;
      if (current <= 0) return v;
      return { ...v, [optionId]: current - 1 };
    });
  }

  /** Toggling veto clears any credits the voter had on that option. */
  function toggleVeto(optionId: string) {
    setVotes((v) => {
      const next = { ...v };
      if (next[optionId] === -1) {
        delete next[optionId];
      } else {
        next[optionId] = -1;
      }
      return next;
    });
  }

  function nextCost(optionId: string): number {
    const v = votes[optionId] ?? 0;
    return voteCost(v + 1, exponent) - (v > 0 ? voteCost(v, exponent) : 0);
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

      {state.runoff && (
        <p className="runoff-banner">Runoff vote — tied options only.</p>
      )}

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
        <p className="field-hint budget-hint">{scalingHint(exponent)}</p>
      </div>

      {error && (
        <p className="error-banner" role="alert">
          {error}
        </p>
      )}

      <ul className="ballot-list">
        {votableOptions.map((o) => {
          const count = votes[o.id] ?? 0;
          const vetoed = count === -1;
          const canPlus = canIncrement(votes, o.id, budget, exponent, vetoCost);
          return (
            <li
              key={o.id}
              className={`ballot-item ${vetoed ? "ballot-item-vetoed" : ""}`}
            >
              <span className="option-title">{o.title}</span>
              {vetoed ? (
                <span className="veto-active-label">Vetoed</span>
              ) : (
                <>
                  <div className="stepper">
                    <button
                      type="button"
                      className="stepper-button"
                      onClick={() => decrement(o.id)}
                      disabled={closed || count <= 0}
                      aria-label={`Decrease votes for ${o.title}`}
                    >
                      −
                    </button>
                    <span className="stepper-count">{count}</span>
                    <button
                      type="button"
                      className="stepper-button"
                      onClick={() => increment(o.id)}
                      disabled={closed || !canPlus}
                      aria-label={`Increase votes for ${o.title}`}
                    >
                      +
                    </button>
                  </div>
                  <span className="cost-hint">next vote costs {nextCost(o.id)}</span>
                </>
              )}
              {vetoEnabled && (
                <button
                  type="button"
                  className="veto-toggle"
                  onClick={() => toggleVeto(o.id)}
                  disabled={closed}
                  aria-label={vetoed ? `Undo veto for ${o.title}` : `Veto ${o.title}`}
                >
                  {vetoed ? "undo veto" : `⛔ veto (−${vetoCost}c)`}
                </button>
              )}
            </li>
          );
        })}
      </ul>

      <div className="actions">
        <button type="button" onClick={handleSubmit} disabled={closed || submitting || left < 0}>
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
