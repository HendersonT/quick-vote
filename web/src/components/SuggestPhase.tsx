import { useState, type FormEvent } from "react";
import { addSuggestion, deleteSuggestion, toggleDoneSuggesting } from "../api";
import type { RoomState } from "../types";

interface SuggestPhaseProps {
  slug: string;
  sessionToken: string;
  state: RoomState;
}

/** Suggestion list + add form for the `suggesting` phase. */
export default function SuggestPhase({ slug, sessionToken, state }: SuggestPhaseProps) {
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [togglingDone, setTogglingDone] = useState(false);

  const you = state.you;
  const mySuggestionCount = you
    ? state.options.filter((o) => o.suggestedById === you.participantId).length
    : 0;
  const cap = state.settings.maxSuggestionsPerUser;
  const capReached = mySuggestionCount >= cap;
  const doneSuggesting = you
    ? (state.participants.find((p) => p.id === you.participantId)?.doneSuggesting ?? false)
    : false;

  function participantName(id: string): string {
    return state.participants.find((p) => p.id === id)?.name ?? "someone";
  }

  async function handleToggleDone() {
    setError(null);
    setTogglingDone(true);
    try {
      await toggleDoneSuggesting(slug, sessionToken);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update done status.");
    } finally {
      setTogglingDone(false);
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = title.trim();
    if (!trimmed) {
      setError("Enter a suggestion.");
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      await addSuggestion(slug, sessionToken, trimmed);
      setTitle("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add suggestion.");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(optionId: string) {
    setError(null);
    try {
      await deleteSuggestion(slug, sessionToken, optionId);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete suggestion.");
    }
  }

  return (
    <section className="suggest-phase">
      <h2>Suggestions</h2>
      {state.options.length === 0 && (
        <p className="empty-hint">No suggestions yet — be the first.</p>
      )}
      <ul className="option-list">
        {state.options.map((o) => (
          <li key={o.id} className="option-item">
            <span className="option-title">{o.title}</span>
            <span className="option-by">by {participantName(o.suggestedById)}</span>
            {you && o.suggestedById === you.participantId && (
              <button
                type="button"
                className="delete-button"
                onClick={() => handleDelete(o.id)}
                aria-label={`Delete ${o.title}`}
              >
                Delete
              </button>
            )}
          </li>
        ))}
      </ul>

      <form className="suggest-form" onSubmit={handleSubmit}>
        {error && (
          <p className="error-banner" role="alert">
            {error}
          </p>
        )}
        <div className="field">
          <label htmlFor="suggestionTitle">Add a suggestion</label>
          <input
            id="suggestionTitle"
            type="text"
            value={title}
            maxLength={200}
            placeholder="Catan"
            disabled={capReached}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>
        <div className="actions">
          <button type="submit" disabled={submitting || capReached}>
            Add
          </button>
          <span className="suggestion-count">
            your {mySuggestionCount} of {cap} used
          </span>
        </div>
      </form>

      {you && (
        <div className="done-suggesting-panel">
          <button type="button" onClick={handleToggleDone} disabled={togglingDone}>
            {doneSuggesting ? "Resume suggesting" : "I'm done suggesting"}
          </button>
          {doneSuggesting && (
            <span className="done-suggesting-note">
              Marked done — you can still add or delete suggestions.
            </span>
          )}
        </div>
      )}
    </section>
  );
}
