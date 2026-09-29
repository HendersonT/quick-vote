import { useEffect, useState } from "react";
import { getVote } from "../api";
import { navigate } from "../App";
import ResultsPhase from "../components/ResultsPhase";
import type { Phase, RoomState } from "../types";
import { connectRoom } from "../ws";

const PHASE_LABELS: Record<Phase, string> = {
  suggesting: "Suggesting",
  voting: "Voting",
  results: "Results",
};

/** One-line description of where a vote is when results aren't in yet. */
function phaseSummary(state: RoomState): string {
  const people = state.participants.length;
  const peopleText = `${people} participant${people === 1 ? "" : "s"}`;
  if (state.phase === "suggesting") {
    const n = state.options.length;
    return `Still collecting suggestions — ${n} so far from ${peopleText}.`;
  }
  const voted = state.participants.filter((p) => p.hasVoted).length;
  return `Voting is underway — ${voted} of ${peopleText} have voted.`;
}

/**
 * `/v/:slug/results` — the read-only results page (spec C2). It renders the
 * spectator snapshot with no join gate, so a results link can be shared with
 * people outside the group; it never creates a session, and the live
 * WebSocket connects as a spectator so the page updates when results land.
 */
export default function ResultsView({ slug }: { slug: string }) {
  const [state, setState] = useState<RoomState | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Fetch first so an unknown slug shows not-found instead of a WebSocket
  // that reconnects forever against a 404.
  useEffect(() => {
    let cancelled = false;
    getVote(slug)
      .then((s) => {
        if (!cancelled) setState(s);
      })
      .catch((err) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : "Vote not found.");
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  const found = state !== null;
  useEffect(() => {
    if (!found) return;
    return connectRoom(slug, "", setState);
  }, [slug, found]);

  if (loadError) {
    return (
      <main className="not-found">
        <h1>Vote not found</h1>
        <p>{loadError}</p>
        <a href="/">Back home</a>
      </main>
    );
  }

  if (!state) {
    return <div className="room-loading">Loading results…</div>;
  }

  const roomPath = `/v/${slug}`;

  return (
    <div className="room results-view">
      <header className="room-header">
        <h1>{state.title}</h1>
        <div className="room-meta">
          <span className={`phase-badge phase-${state.phase}`}>
            {PHASE_LABELS[state.phase]}
          </span>
        </div>
      </header>

      {state.closed && (
        <p className="closed-banner" role="status">
          This vote is closed.
        </p>
      )}

      {state.phase === "results" ? (
        <ResultsPhase
          readOnly
          slug={slug}
          sessionToken=""
          state={state}
          closed={state.closed}
        />
      ) : (
        <section className="results-pending">
          <h2>Results aren't in yet</h2>
          <p>{phaseSummary(state)}</p>
          {!state.closed && (
            <a
              className="button-link"
              href={roomPath}
              onClick={(e) => {
                e.preventDefault();
                navigate(roomPath);
              }}
            >
              Join this vote
            </a>
          )}
        </section>
      )}
    </div>
  );
}
