import { useState, type FormEvent } from "react";
import { joinVote } from "../api";
import type { RoomState } from "../types";

interface JoinGateProps {
  slug: string;
  /** The vote's title, fetched via a spectator GET before joining, if known. */
  title?: string;
  onJoined: (sessionToken: string, name: string, state: RoomState) => void;
}

/** Name-entry gate shown to anyone without a saved session for this vote. */
export default function JoinGate({ slug, title, onJoined }: JoinGateProps) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Enter your name.");
      return;
    }
    if (trimmed.length > 50) {
      setError("Name must be 50 characters or fewer.");
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const { sessionToken, state } = await joinVote(slug, trimmed);
      onJoined(sessionToken, trimmed, state);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to join.");
      setSubmitting(false);
    }
  }

  return (
    <main className="join-gate">
      <h1>{title ?? "Join vote"}</h1>
      <p className="join-gate-tagline">Enter your name to join.</p>
      <form onSubmit={handleSubmit}>
        {error && (
          <p className="error-banner" role="alert">
            {error}
          </p>
        )}
        <div className="field">
          <label htmlFor="joinName">Your name</label>
          <input
            id="joinName"
            type="text"
            value={name}
            maxLength={50}
            placeholder="Sam"
            onChange={(e) => setName(e.target.value)}
            autoFocus
            required
          />
        </div>
        <div className="actions">
          <button type="submit" disabled={submitting}>
            {submitting ? "Joining…" : "Join"}
          </button>
        </div>
      </form>
    </main>
  );
}
