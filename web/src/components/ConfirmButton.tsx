import { useState } from "react";

interface ConfirmButtonProps {
  label: string;
  /** Label of the confirming button once armed. Defaults to "Yes". */
  confirmLabel?: string;
  onConfirm: () => Promise<void> | void;
  disabled?: boolean;
  className?: string;
  /** Accessible name for the initial button, when `label` is terse (e.g. "×"). */
  ariaLabel?: string;
}

/**
 * Two-step inline confirm for destructive or phase-changing actions: the
 * first click swaps the button for "Sure? Yes / Cancel" in place. Replaces
 * window.confirm, which is blocking, unstyled, and suppressed by some
 * mobile browsers.
 */
export default function ConfirmButton({
  label,
  confirmLabel = "Yes",
  onConfirm,
  disabled,
  className,
  ariaLabel,
}: ConfirmButtonProps) {
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const classes = ["confirm-button", className].filter(Boolean).join(" ");

  if (!armed) {
    return (
      <button
        type="button"
        className={classes}
        disabled={disabled}
        aria-label={ariaLabel}
        onClick={() => setArmed(true)}
      >
        {label}
      </button>
    );
  }

  async function handleConfirm() {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
      setArmed(false);
    }
  }

  return (
    <span className="confirm-group" role="group" aria-label={ariaLabel ?? label}>
      <span className="confirm-prompt">Sure?</span>
      <button
        type="button"
        className={`${classes} confirm-yes`}
        disabled={disabled || busy}
        onClick={handleConfirm}
        autoFocus
      >
        {confirmLabel}
      </button>
      <button
        type="button"
        className="confirm-button confirm-cancel"
        disabled={busy}
        onClick={() => setArmed(false)}
      >
        Cancel
      </button>
    </span>
  );
}
