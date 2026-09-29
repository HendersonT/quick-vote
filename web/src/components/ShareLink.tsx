import { useState } from "react";
import { copyText } from "../clipboard";
import QrCode from "./QrCode";

/**
 * Copy-to-clipboard button for the room's shareable URL, plus a toggle that
 * shows the same URL as a QR code for people in the room (spec C3).
 */
export default function ShareLink({ slug }: { slug: string }) {
  const [copied, setCopied] = useState(false);
  const [showQr, setShowQr] = useState(false);
  const url = `${window.location.origin}/v/${slug}`;

  async function handleCopy() {
    if (await copyText(url, "Copy this link")) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }

  return (
    <>
      <button type="button" className="share-link" onClick={handleCopy}>
        {copied ? "Copied!" : "Copy share link"}
      </button>
      <button
        type="button"
        className="share-link"
        aria-expanded={showQr}
        onClick={() => setShowQr((v) => !v)}
      >
        {showQr ? "Hide QR" : "Show QR"}
      </button>
      {showQr && (
        <div className="qr-panel">
          <QrCode url={url} />
        </div>
      )}
    </>
  );
}
