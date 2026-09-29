import { useState } from "react";
import { advanceVote, toggleRevote } from "../api";
import { copyText } from "../clipboard";
import type { RoomState } from "../types";

interface ResultsPhaseProps {
  slug: string;
  sessionToken: string;
  creatorToken?: string;
  state: RoomState;
  /** True while the vote is closed: re-vote and tiebreak are disabled (spec B3). */
  closed?: boolean;
  /**
   * The shareable results page (spec C2): no session, so the re-vote panel
   * and the creator's tiebreak buttons are hidden rather than shown disabled.
   */
  readOnly?: boolean;
}

/**
 * Score bars, winner/tie banner, export/share actions, and the re-vote toggle
 * for `results`.
 */
export default function ResultsPhase({
  slug,
  sessionToken,
  creatorToken,
  state,
  closed = false,
  readOnly = false,
}: ResultsPhaseProps) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);

  const results = state.results;
  const isCreator = state.you?.isCreator ?? false;
  const wantsRevote = state.you
    ? (state.participants.find((p) => p.id === state.you!.participantId)?.wantsRevote ?? false)
    : false;

  function participantName(id: string): string {
    return state.participants.find((p) => p.id === id)?.name ?? "someone";
  }

  function optionTitle(id: string): string {
    return state.options.find((o) => o.id === id)?.title ?? "";
  }

  async function handlePickWinner(optionId: string) {
    if (!creatorToken) return;
    setError(null);
    setBusy(true);
    try {
      await advanceVote(slug, sessionToken, creatorToken, optionId);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to set winner.");
    } finally {
      setBusy(false);
    }
  }

  async function handleToggleRevote() {
    setError(null);
    setBusy(true);
    try {
      await toggleRevote(slug, sessionToken);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to call for re-vote.");
    } finally {
      setBusy(false);
    }
  }

  async function handleCopyResultsLink() {
    const url = `${window.location.origin}/v/${slug}/results`;
    if (await copyText(url, "Copy the results link")) {
      setLinkCopied(true);
      setTimeout(() => setLinkCopied(false), 2000);
    }
  }

  if (!results) {
    return <p className="phase-placeholder">Tallying results…</p>;
  }

  const maxScore = Math.max(1, ...results.scores.map((s) => s.score));

  return (
    <section className="results-phase">
      <h2>Results</h2>

      {error && (
        <p className="error-banner" role="alert">
          {error}
        </p>
      )}

      {results.tiePending ? (
        <div className="winner-banner tie-pending">
          <p>Tied — waiting for the creator to pick.</p>
          {isCreator && !readOnly && (
            <ul className="tie-options">
              {results.tiedOptionIds.map((id) => (
                <li key={id}>
                  <button
                    type="button"
                    className="tie-option-button"
                    disabled={busy || closed}
                    onClick={() => handlePickWinner(id)}
                  >
                    Pick {optionTitle(id)}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      ) : results.winnerOptionId ? (
        <div className="winner-banner">
          <p>
            Winner: <strong>{optionTitle(results.winnerOptionId)}</strong>
          </p>
        </div>
      ) : (
        <div className="winner-banner no-winner">
          <p>No winner — every option was vetoed.</p>
        </div>
      )}

      {results.tiebreakNote && <p className="tiebreak-note">{results.tiebreakNote}</p>}

      <ul className="score-list">
        {results.scores.map((s) => (
          <li
            key={s.optionId}
            className={`score-item ${s.eliminated ? "score-item-eliminated" : ""} ${
              !s.eliminated && s.optionId === results.winnerOptionId ? "score-item-winner" : ""
            }`}
          >
            <div className="score-item-header">
              <span className="option-title">{optionTitle(s.optionId)}</span>
              {s.vetoCount > 0 ? (
                <span className="veto-badge veto-badge-explicit">
                  vetoed by {s.vetoCount} voter{s.vetoCount === 1 ? "" : "s"}
                </span>
              ) : (
                s.eliminated && (
                  <span className="veto-badge">
                    eliminated — under {state.settings.survivalThreshold} credits
                  </span>
                )
              )}
            </div>
            <div className="score-bar-track">
              <div
                className="score-bar-fill"
                style={{ width: `${(Math.max(0, s.score) / maxScore) * 100}%` }}
              />
            </div>
            <span className="score-detail">
              {s.score} credits · {s.backers} backer{s.backers === 1 ? "" : "s"}
            </span>
          </li>
        ))}
      </ul>

      <div className="results-actions">
        {/* Plain same-origin link: the server's Content-Disposition makes it a
            download, so no JS blob handling is needed (spec C1). */}
        <a className="button-link" href={`/api/votes/${slug}/results.csv`}>
          Download CSV
        </a>
        <button type="button" className="share-link" onClick={handleCopyResultsLink}>
          {linkCopied ? "Copied!" : "Copy results link"}
        </button>
      </div>

      {!readOnly && (
        <div className="revote-panel">
          <button type="button" onClick={handleToggleRevote} disabled={busy || closed}>
            {wantsRevote ? "Withdraw call" : "Call for re-vote"}
          </button>
          <span className="revote-count">
            {results.revoteCalls} of {results.revoteNeeded} needed
          </span>
          {results.revoteCalls > 0 && (
            <p className="revote-callers">
              Called by:{" "}
              {state.participants
                .filter((p) => p.wantsRevote)
                .map((p) => participantName(p.id))
                .join(", ")}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
