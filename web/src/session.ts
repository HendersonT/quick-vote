import type { RoomState } from "./types";

export interface Session {
  sessionToken: string;
  creatorToken?: string;
  name: string;
}

const key = (slug: string) => `qv:${slug}`;

export function getSession(slug: string): Session | null {
  const raw = localStorage.getItem(key(slug));
  if (!raw) return null;
  try {
    return JSON.parse(raw) as Session;
  } catch {
    return null;
  }
}

export function saveSession(slug: string, session: Session): void {
  localStorage.setItem(key(slug), JSON.stringify(session));
}

export function clearSession(slug: string): void {
  localStorage.removeItem(key(slug));
}

// Client-side "recent votes" history (F6 — no backend change: votes/results
// are already retained indefinitely, this just gives the browser a way back
// to a room without keeping the link around).

export interface HistoryEntry {
  slug: string;
  title: string;
  ts: number;
  // closed is the vote's closed flag as last seen by this browser; closed
  // votes are labeled and listed below open ones (spec B3).
  closed?: boolean;
}

const HISTORY_KEY = "qv:history";
const HISTORY_CAP = 20;

/** Most-recent-first list of votes this browser has created or joined. */
export function getHistory(): HistoryEntry[] {
  const raw = localStorage.getItem(HISTORY_KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? (parsed as HistoryEntry[]) : [];
  } catch {
    return [];
  }
}

/**
 * Records (or bumps to the front of) the recent-votes history. Deduped by
 * slug — rejoining/reloading a room moves it to the top rather than adding a
 * duplicate entry — and capped at HISTORY_CAP most-recent entries.
 */
export function addToHistory(slug: string, title: string): void {
  const history = getHistory();
  // Bumping keeps the last-seen closed flag; only a snapshot changes it.
  const closed = history.find((e) => e.slug === slug)?.closed;
  const rest = history.filter((e) => e.slug !== slug);
  const entry: HistoryEntry = { slug, title, ts: Date.now() };
  if (closed !== undefined) entry.closed = closed;
  const next = [entry, ...rest].slice(0, HISTORY_CAP);
  localStorage.setItem(HISTORY_KEY, JSON.stringify(next));
}

/**
 * Records the last-seen closed flag on a history entry without reordering
 * (visiting a closed vote shouldn't bump it to the top). No-op when the slug
 * isn't in history or the flag is unchanged, so calling this on every
 * snapshot doesn't rewrite storage on every broadcast.
 */
export function setHistoryClosed(slug: string, closed: boolean): void {
  const history = getHistory();
  const entry = history.find((e) => e.slug === slug);
  if (!entry || (entry.closed ?? false) === closed) return;
  entry.closed = closed;
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
}

/** Open votes first (most recent first), then closed ones (most recent first). */
export function sortHistory(entries: HistoryEntry[]): HistoryEntry[] {
  return [...entries].sort((a, b) => {
    const ac = a.closed ? 1 : 0;
    const bc = b.closed ? 1 : 0;
    return ac !== bc ? ac - bc : b.ts - a.ts;
  });
}

// Next-vote handoff (spec B4). When the creator starts a follow-up vote, the
// old room's snapshot carries each participant's new session token; the
// browser saves it and follows the group. The "moved" marker makes that
// automatic follow happen only once per old vote: without it, going back to
// the old vote (history, back button, an old link) would bounce straight to
// the new one again — a redirect loop the user can't escape.

const movedKey = (slug: string) => `qv:moved:${slug}`;

/** Marks oldSlug as already followed, so it never auto-moves again. */
export function markMoved(oldSlug: string): void {
  localStorage.setItem(movedKey(oldSlug), "1");
}

/**
 * True when this browser should auto-follow the group to `state.next` —
 * only once per old slug, so revisiting an old vote never redirect-loops.
 * Spectators and removed participants (no handoff token) never auto-move.
 */
export function shouldAutoMove(oldSlug: string, state: RoomState): boolean {
  return (
    !!state.next &&
    !!state.you?.nextSessionToken &&
    localStorage.getItem(movedKey(oldSlug)) === null
  );
}

/**
 * Saves the next-vote session (with the new creator token for the creator),
 * marks oldSlug as moved, and records the new vote in history. Returns the
 * next vote's slug. Callers check shouldAutoMove first.
 */
export function applyMove(oldSlug: string, state: RoomState, name: string): string {
  const next = state.next!;
  const session: Session = { sessionToken: state.you!.nextSessionToken!, name };
  if (state.you?.nextCreatorToken) session.creatorToken = state.you.nextCreatorToken;
  saveSession(next.slug, session);
  markMoved(oldSlug);
  addToHistory(next.slug, next.title);
  return next.slug;
}
