import { useState, type FormEvent } from "react";
import { createNextVote } from "../api";
import { navigate } from "../App";
import { addToHistory, markMoved, saveSession } from "../session";
import type { RoomState } from "../types";
import SettingsFields, { fromSettings, toSettings, type SettingsFormState } from "./SettingsFields";

interface NextVoteFormProps {
  slug: string;
  sessionToken: string;
  creatorToken: string;
  name: string;
  state: RoomState;
  onCancel: () => void;
}

/**
 * Creator form for "Start another vote with this group" (spec B4). Settings
 * start from the current vote's; the server copies every current participant
 * into the new vote and hands each of them a session via the old room's
 * snapshot, so the group follows automatically.
 */
export default function NextVoteForm({
  slug,
  sessionToken,
  creatorToken,
  name,
  state,
  onCancel,
}: NextVoteFormProps) {
  const [title, setTitle] = useState("");
  const [settings, setSettings] = useState<SettingsFormState>(() =>
    fromSettings(state.settings),
  );
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = title.trim();
    if (!trimmed) {
      setError("Give the next vote a title.");
      return;
    }
    if (trimmed.length > 200) {
      setError("Title must be 200 characters or fewer.");
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const res = await createNextVote(
        slug,
        sessionToken,
        creatorToken,
        trimmed,
        toSettings(settings),
      );
      saveSession(res.slug, {
        sessionToken: res.sessionToken,
        creatorToken: res.creatorToken,
        name,
      });
      // The old room's next broadcast will carry the same handoff; marking
      // it moved now keeps that snapshot from triggering a second move.
      markMoved(slug);
      addToHistory(res.slug, trimmed);
      navigate(`/v/${res.slug}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start the next vote.");
      setSubmitting(false);
    }
  }

  return (
    <form className="next-vote-form" onSubmit={handleSubmit}>
      <h2>Start another vote with this group</h2>
      <p className="field-hint">
        Everyone here moves to the new vote automatically.
      </p>
      {error && (
        <p className="error-banner" role="alert">
          {error}
        </p>
      )}
      <div className="field">
        <label htmlFor="nextTitle">Vote title</label>
        <input
          id="nextTitle"
          type="text"
          value={title}
          maxLength={200}
          placeholder="Round two"
          onChange={(e) => setTitle(e.target.value)}
          autoFocus
          required
        />
      </div>
      <SettingsFields value={settings} onChange={setSettings} />
      <div className="actions">
        <button type="submit" disabled={submitting}>
          {submitting ? "Creating…" : "Create"}
        </button>
        <button type="button" className="secondary-button" onClick={onCancel} disabled={submitting}>
          Cancel
        </button>
      </div>
    </form>
  );
}
