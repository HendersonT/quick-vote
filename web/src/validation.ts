// Client-side checks for vote titles and participant names, shared by every
// form that submits one so the limits and messages can't drift apart. The
// server enforces the same limits; checking here just saves a round trip.

export const MAX_TITLE_CHARS = 200;
export const MAX_NAME_CHARS = 50;

export type Checked = { ok: true; value: string } | { ok: false; error: string };

/**
 * Length in characters (code points), which is how the server counts
 * (utf8.RuneCountInString). Note the inputs' maxLength attribute still
 * counts UTF-16 units, so in the UI an emoji uses two of the limit; that
 * only makes the browser stricter than the server, never looser.
 */
function charCount(s: string): number {
  return [...s].length;
}

/** Trims a vote title and checks it is present and within the limit. */
export function checkTitle(raw: string): Checked {
  const value = raw.trim();
  if (!value) return { ok: false, error: "Give the vote a title." };
  if (charCount(value) > MAX_TITLE_CHARS) {
    return { ok: false, error: `Title must be ${MAX_TITLE_CHARS} characters or fewer.` };
  }
  return { ok: true, value };
}

/** Trims a participant name and checks it is present and within the limit. */
export function checkName(raw: string): Checked {
  const value = raw.trim();
  if (!value) return { ok: false, error: "Enter your name." };
  if (charCount(value) > MAX_NAME_CHARS) {
    return { ok: false, error: `Name must be ${MAX_NAME_CHARS} characters or fewer.` };
  }
  return { ok: true, value };
}
