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
  const rest = getHistory().filter((e) => e.slug !== slug);
  const next = [{ slug, title, ts: Date.now() }, ...rest].slice(0, HISTORY_CAP);
  localStorage.setItem(HISTORY_KEY, JSON.stringify(next));
}
