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
