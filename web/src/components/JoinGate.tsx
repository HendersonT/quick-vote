import { useState, type FormEvent } from "react";
import { joinVote } from "../api";
import { navigate } from "../App";
import type { RoomState } from "../types";

interface JoinGateProps {
  slug: string;
  /** The vote's title, fetched via a spectator GET before joining, if known. */
  title?: string;
  onJoined: (sessionToken: string, name: string, state: RoomState) => void;
  /** Informational banner above the form (e.g. "you're no longer in this vote"). */
  notice?: string;
  /** Follow-up vote this group moved on to, if any (spec B4). */
  next?: { slug: string; title: string } | null;
  /** True when the vote is closed and joining will be refused (spec B3). */
  closed?: boolean;
}

/** Name-entry gate shown to anyone without a saved session for this vote. */
export default function JoinGate({ slug, title, onJoined, notice, next, closed }: JoinGateProps) {
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
      {notice && (
        <p className="info-banner" role="status">
          {notice}
        </p>
      )}
      {closed && (
        <p className="closed-banner" role="status">
          This vote is closed.
        </p>
      )}
      {next && <MovedOnBanner next={next} />}
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

/** "This group moved on → title" link to the follow-up vote (spec B4). */
export function MovedOnBanner({ next }: { next: { slug: string; title: string } }) {
  return (
    <p className="info-banner" role="status">
      This group moved on →{" "}
      <a
        href={`/v/${next.slug}`}
        onClick={(e) => {
          e.preventDefault();
          navigate(`/v/${next.slug}`);
        }}
      >
        {next.title}
      </a>
    </p>
  );
}
