import { useEffect, useRef, useState } from "react";
import { advanceVote, closeVote, getVote, removeParticipant, reopenVote } from "../api";
import { navigate } from "../App";
import ConfirmButton from "../components/ConfirmButton";
import Countdown from "../components/Countdown";
import JoinGate, { MovedOnBanner } from "../components/JoinGate";
import LoadFailed from "../components/LoadFailed";
import NextVoteForm from "../components/NextVoteForm";
import ParticipantList from "../components/ParticipantList";
import ResultsPhase from "../components/ResultsPhase";
import ShareLink from "../components/ShareLink";
import SuggestPhase from "../components/SuggestPhase";
import VotePhase from "../components/VotePhase";
import {
  addToHistory,
  applyMove,
  clearSession,
  getSession,
  saveSession,
  setHistoryClosed,
  shouldAutoMove,
  type Session,
} from "../session";
import type { Phase, RoomState } from "../types";
import { connectRoom } from "../ws";

interface RoomProps {
  slug: string;
}

// One-time notice carried across the next-vote navigation ("Moved to the
// next vote: …"). sessionStorage so it survives the route change but not a
// new tab; storage can throw (private mode, blocked site data), in which
// case the notice is simply skipped.
const NOTICE_KEY = "qv:notice";

function peekNotice(): string | null {
  try {
    return sessionStorage.getItem(NOTICE_KEY);
  } catch {
    return null;
  }
}

function stashNotice(text: string): void {
  try {
    sessionStorage.setItem(NOTICE_KEY, text);
  } catch {
    // best effort
  }
}

function dropNotice(): void {
  try {
    sessionStorage.removeItem(NOTICE_KEY);
  } catch {
    // best effort
  }
}

interface PreJoinInfo {
  title: string;
  closed: boolean;
  next: RoomState["next"];
}

const PHASE_LABELS: Record<Phase, string> = {
  suggesting: "Suggesting",
  voting: "Voting",
  results: "Results",
};

export default function Room({ slug }: RoomProps) {
  const [session, setSession] = useState<Session | null>(() => getSession(slug));
  const [state, setState] = useState<RoomState | null>(null);
  const [preJoin, setPreJoin] = useState<PreJoinInfo | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [controlError, setControlError] = useState<string | null>(null);
  const [controlBusy, setControlBusy] = useState(false);
  const [showNextForm, setShowNextForm] = useState(false);
  // Set when a live session stops resolving (removed by the creator, or the
  // server no longer knows the token): the join gate explains why.
  const [removed, setRemoved] = useState(false);
  // Read on mount, removed in an effect (not the initializer) so StrictMode's
  // double-invoked initializer can't consume it before the real render.
  const [notice, setNotice] = useState<string | null>(peekNotice);

  useEffect(() => {
    dropNotice();
  }, []);
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
        if (!cancelled) setPreJoin({ title: s.title, closed: s.closed, next: s.next });
      })
      .catch((err) => {
        if (!cancelled) setLoadError(err ?? new Error("load failed"));
      });
    return () => {
      cancelled = true;
    };
  }, [slug, session]);

  useEffect(() => {
    if (!session) return;
    return connectRoom(slug, session.sessionToken, (next) => {
      // The group moved on to a follow-up vote and this snapshot carries our
      // session for it: follow once (shouldAutoMove guards against looping
      // back here if the user later revisits this vote).
      if (shouldAutoMove(slug, next)) {
        const to = applyMove(slug, next, session.name);
        stashNotice(`Moved to the next vote: ${next.next!.title}`);
        navigate(`/v/${to}`);
        return;
      }
      // If a stored session token is no longer valid (removed by the creator,
      // server DB reset, etc.) the server still connects us but as a
      // spectator (you === null). Rather than silently trapping the user in a
      // read-only room with no rejoin affordance, drop the dead session and
      // fall back to the join gate with an explanation so they can re-enter.
      if (next.you === null) {
        clearSession(slug);
        setSession(null);
        setState(null);
        setRemoved(true);
        // This snapshot already says whether the vote is closed, so the
        // gate can word its notice right without waiting for a refetch.
        setPreJoin({ title: next.title, closed: next.closed, next: next.next });
        return;
      }
      setState(next);
      if (!historyRecorded.current) {
        historyRecorded.current = true;
        addToHistory(slug, next.title);
      }
      setHistoryClosed(slug, next.closed);
    }, session.creatorToken);
  }, [slug, session]);

  function handleJoined(sessionToken: string, name: string, joinedState: RoomState) {
    saveSession(slug, { sessionToken, name });
    setSession({ sessionToken, name });
    setState(joinedState);
    setRemoved(false);
    historyRecorded.current = true;
    addToHistory(slug, joinedState.title);
    setHistoryClosed(slug, joinedState.closed);
  }

  /**
   * Runs a creator action and applies the returned room state. Errors land in
   * the creator-controls banner; they never reject, so ConfirmButton callers
   * don't leak unhandled rejections.
   */
  async function runCreatorAction(
    action: (sessionToken: string, creatorToken: string) => Promise<RoomState>,
    fallbackError: string,
  ) {
    if (!session?.creatorToken) return;
    setControlError(null);
    setControlBusy(true);
    try {
      const next = await action(session.sessionToken, session.creatorToken);
      setState(next);
    } catch (err) {
      setControlError(err instanceof Error ? err.message : fallbackError);
    } finally {
      setControlBusy(false);
    }
  }

  const handleAdvance = () =>
    runCreatorAction((st, ct) => advanceVote(slug, st, ct), "Failed to advance.");
  const handleClose = () =>
    runCreatorAction((st, ct) => closeVote(slug, st, ct), "Failed to close the vote.");
  const handleReopen = () =>
    runCreatorAction((st, ct) => reopenVote(slug, st, ct), "Failed to reopen the vote.");
  const handleRemove = (participantId: string) =>
    runCreatorAction(
      (st, ct) => removeParticipant(slug, st, ct, participantId),
      "Failed to remove participant.",
    );

  if (!session) {
    if (loadError) {
      return <LoadFailed error={loadError} what="this vote" />;
    }
    return (
      <JoinGate
        slug={slug}
        title={preJoin?.title}
        onJoined={handleJoined}
        removed={removed}
        next={preJoin?.next}
        closed={preJoin?.closed}
      />
    );
  }

  if (!state) {
    return <div className="room-loading">Loading vote…</div>;
  }

  const creatorToken = state.you?.isCreator ? session.creatorToken : undefined;
  const isCreator = !!creatorToken;
  const closed = state.closed;

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
            {controlError && (
              <p className="error-banner" role="alert">
                {controlError}
              </p>
            )}
            {!closed && (
              <ConfirmButton
                label="Advance phase"
                confirmLabel="Advance"
                onConfirm={handleAdvance}
                disabled={controlBusy}
              />
            )}
            {closed ? (
              <button type="button" onClick={handleReopen} disabled={controlBusy}>
                Reopen vote
              </button>
            ) : (
              <ConfirmButton
                label="Close vote"
                confirmLabel="Close"
                onConfirm={handleClose}
                disabled={controlBusy}
              />
            )}
            {!closed && !state.next && (
              <button
                type="button"
                aria-expanded={showNextForm}
                onClick={() => setShowNextForm((v) => !v)}
              >
                Start another vote with this group
              </button>
            )}
          </div>
        )}
        {isCreator && showNextForm && !closed && !state.next && (
          <NextVoteForm
            slug={slug}
            sessionToken={session.sessionToken}
            creatorToken={creatorToken}
            name={session.name}
            state={state}
            onCancel={() => setShowNextForm(false)}
          />
        )}
      </header>

      {notice && (
        <div className="info-banner info-banner-dismissible" role="status">
          <span>{notice}</span>
          <button
            type="button"
            className="banner-dismiss"
            aria-label="Dismiss"
            onClick={() => setNotice(null)}
          >
            ×
          </button>
        </div>
      )}
      {closed && (
        <p className="closed-banner" role="status">
          This vote is closed.
        </p>
      )}
      {state.next && <MovedOnBanner next={state.next} />}

      <div className="room-body">
        <ParticipantList
          participants={state.participants}
          phase={state.phase}
          canRemove={isCreator && !closed}
          onRemove={handleRemove}
        />
        <main className="room-content">
          {state.phase === "suggesting" && (
            <SuggestPhase
              slug={slug}
              sessionToken={session.sessionToken}
              creatorToken={creatorToken}
              state={state}
              closed={closed}
            />
          )}
          {state.phase === "voting" && (
            <VotePhase
              slug={slug}
              sessionToken={session.sessionToken}
              state={state}
              closed={closed}
            />
          )}
          {state.phase === "results" && (
            <ResultsPhase
              slug={slug}
              sessionToken={session.sessionToken}
              creatorToken={creatorToken}
              state={state}
              closed={closed}
            />
          )}
        </main>
      </div>
    </div>
  );
}
