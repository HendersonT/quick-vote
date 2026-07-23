import { useEffect, useRef, useState } from "react";
import { advanceVote, getVote } from "../api";
import Countdown from "../components/Countdown";
import JoinGate from "../components/JoinGate";
import ParticipantList from "../components/ParticipantList";
import ResultsPhase from "../components/ResultsPhase";
import ShareLink from "../components/ShareLink";
import SuggestPhase from "../components/SuggestPhase";
import VotePhase from "../components/VotePhase";
import { addToHistory, clearSession, getSession, saveSession, type Session } from "../session";
import type { Phase, RoomState } from "../types";
import { connectRoom } from "../ws";

interface RoomProps {
  slug: string;
}

const PHASE_LABELS: Record<Phase, string> = {
  suggesting: "Suggesting",
  voting: "Voting",
  results: "Results",
};

export default function Room({ slug }: RoomProps) {
  const [session, setSession] = useState<Session | null>(() => getSession(slug));
  const [state, setState] = useState<RoomState | null>(null);
  const [preJoinTitle, setPreJoinTitle] = useState<string | undefined>(undefined);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [advanceError, setAdvanceError] = useState<string | null>(null);
  const [advancing, setAdvancing] = useState(false);
  // Guards against re-recording history on every WS broadcast — only the
  // first snapshot after (re)joining this room needs to bump it (F6).
  const historyRecorded = useRef(false);

  // Before joining, fetch a spectator snapshot so the join gate can show the
  // vote's title and so an unknown slug surfaces a friendly error.
  useEffect(() => {
    if (session) return;
    let cancelled = false;
    getVote(slug)
      .then((s) => {
        if (!cancelled) setPreJoinTitle(s.title);
      })
      .catch((err) => {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : "Vote not found.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [slug, session]);

  useEffect(() => {
    if (!session) return;
    return connectRoom(slug, session.sessionToken, (next) => {
      // If a stored session token is no longer valid (server DB reset, pruned
      // vote, etc.) the server still connects us but as a spectator (you ===
      // null). Rather than silently trapping the user in a read-only room with
      // no rejoin affordance, drop the dead session and fall back to the join
      // gate so they can re-enter.
      if (next.you === null) {
        clearSession(slug);
        setSession(null);
        setState(null);
        return;
      }
      setState(next);
      if (!historyRecorded.current) {
        historyRecorded.current = true;
        addToHistory(slug, next.title);
      }
    });
  }, [slug, session]);

  function handleJoined(sessionToken: string, name: string, joinedState: RoomState) {
    saveSession(slug, { sessionToken, name });
    setSession({ sessionToken, name });
    setState(joinedState);
    historyRecorded.current = true;
    addToHistory(slug, joinedState.title);
  }

  async function handleAdvance() {
    if (!session?.creatorToken) return;
    if (!window.confirm("Advance to the next phase?")) return;
    setAdvanceError(null);
    setAdvancing(true);
    try {
      const next = await advanceVote(slug, session.sessionToken, session.creatorToken);
      setState(next);
    } catch (err) {
      setAdvanceError(err instanceof Error ? err.message : "Failed to advance.");
    } finally {
      setAdvancing(false);
    }
  }

  if (!session) {
    if (loadError) {
      return (
        <main className="not-found">
          <h1>Vote not found</h1>
          <p>{loadError}</p>
          <a href="/">Back home</a>
        </main>
      );
    }
    return <JoinGate slug={slug} title={preJoinTitle} onJoined={handleJoined} />;
  }

  if (!state) {
    return <div className="room-loading">Loading vote…</div>;
  }

  const isCreator = state.you?.isCreator ?? false;

  return (
    <div className="room">
      <header className="room-header">
        <h1>{state.title}</h1>
        <div className="room-meta">
          <span className={`phase-badge phase-${state.phase}`}>
            {PHASE_LABELS[state.phase]}
          </span>
          {state.phaseDeadline && <Countdown deadline={state.phaseDeadline} />}
          <ShareLink slug={slug} />
        </div>
        {isCreator && (
          <div className="creator-controls">
            {advanceError && (
              <p className="error-banner" role="alert">
                {advanceError}
              </p>
            )}
            <button type="button" onClick={handleAdvance} disabled={advancing}>
              {advancing ? "Advancing…" : "Advance phase"}
            </button>
          </div>
        )}
      </header>

      <div className="room-body">
        <ParticipantList participants={state.participants} phase={state.phase} />
        <main className="room-content">
          {state.phase === "suggesting" && (
            <SuggestPhase slug={slug} sessionToken={session.sessionToken} state={state} />
          )}
          {state.phase === "voting" && (
            <VotePhase slug={slug} sessionToken={session.sessionToken} state={state} />
          )}
          {state.phase === "results" && (
            <ResultsPhase
              slug={slug}
              sessionToken={session.sessionToken}
              creatorToken={session.creatorToken}
              state={state}
            />
          )}
        </main>
      </div>
    </div>
  );
}
