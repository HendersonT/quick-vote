import { useEffect, useState } from "react";
import QRCode from "qrcode";

const SIZE = 192;

/**
 * Renders `url` as a QR code, drawn client-side to a data-URL <img> so no
 * external service sees the join link and the CSP (`img-src 'self' data:`)
 * needs no changes (spec C3).
 */
export default function QrCode({ url }: { url: string }) {
  const [dataUrl, setDataUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setDataUrl(null);
    setFailed(false);
    QRCode.toDataURL(url, { margin: 1, width: SIZE })
      .then((d) => {
        if (!cancelled) setDataUrl(d);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [url]);

  if (failed) {
    return <p className="qr-error">Couldn't draw the QR code.</p>;
  }
  if (!dataUrl) {
    // Same footprint as the finished image so the header doesn't jump.
    return <div className="qr-placeholder" style={{ width: SIZE, height: SIZE }} />;
  }
  return (
    <img
      className="qr-code"
      alt="QR code for the join link"
      src={dataUrl}
      width={SIZE}
      height={SIZE}
    />
  );
}
