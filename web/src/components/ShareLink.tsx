import { useState } from "react";

/** Copy-to-clipboard button for the room's shareable URL. */
export default function ShareLink({ slug }: { slug: string }) {
  const [copied, setCopied] = useState(false);
  const url = `${window.location.origin}/v/${slug}`;

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API unavailable (older browser, insecure context, denied
      // permission) — fall back to a manual copy prompt.
      window.prompt("Copy this link", url);
    }
  }

  return (
    <button type="button" className="share-link" onClick={handleCopy}>
      {copied ? "Copied!" : "Copy share link"}
    </button>
  );
}
