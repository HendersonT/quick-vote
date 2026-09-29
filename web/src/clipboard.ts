/**
 * Copies `text` to the clipboard. Returns true on success. When the Clipboard
 * API is unavailable (older browser, insecure context, denied permission) it
 * falls back to a manual copy prompt and returns false, so callers only show
 * "Copied!" when the copy actually happened.
 */
export async function copyText(text: string, promptLabel: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    window.prompt(promptLabel, text);
    return false;
  }
}
