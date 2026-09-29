import { useState, type FormEvent } from "react";
import { createNextVote } from "../api";
import { navigate } from "../App";
import { markMoved, recordMove, unmarkMoved } from "../session";
import type { RoomState } from "../types";
import { checkTitle, MAX_TITLE_CHARS } from "../validation";
import SettingsFields, { fromSettings, toSettings, type SettingsFormState } from "./SettingsFields";

interface NextVoteFormProps {
  /** Element id, referenced by the toggle's aria-controls. */
  id?: string;
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
  id,
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
    const checked = checkTitle(title);
    if (!checked.ok) {
      setError(checked.error);
      return;
    }
    setError(null);
    setSubmitting(true);
    // Mark the old vote moved before the request, not after: the server
    // broadcasts the handoff to the old room (this tab's socket included)
    // before the response arrives, and an unmarked snapshot would make Room
    // follow on its own and then this handler navigate a second time.
    markMoved(slug);
    try {
      const res = await createNextVote(
        slug,
        sessionToken,
        creatorToken,
        checked.value,
        toSettings(settings),
      );
      recordMove(
        slug,
        { slug: res.slug, title: checked.value },
        { sessionToken: res.sessionToken, creatorToken: res.creatorToken, name },
      );
      navigate(`/v/${res.slug}`);
    } catch (err) {
      unmarkMoved(slug);
      setError(err instanceof Error ? err.message : "Failed to start the next vote.");
      setSubmitting(false);
    }
  }

  return (
    <form id={id} className="next-vote-form" onSubmit={handleSubmit}>
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
          maxLength={MAX_TITLE_CHARS}
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
