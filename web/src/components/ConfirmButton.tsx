import { useEffect, useRef, useState } from "react";

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
 * mobile browsers. Focus returns to the trigger when the prompt closes; it is
 * lost only when the action removes the trigger itself (e.g. the removed
 * participant's row).
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
  const triggerRef = useRef<HTMLButtonElement>(null);
  // Set when the prompt closes. The Yes/Cancel button that had focus
  // unmounts with it, dropping keyboard focus to the page body, so the
  // trigger that replaces it takes focus back.
  const refocus = useRef(false);
  const classes = ["confirm-button", className].filter(Boolean).join(" ");

  useEffect(() => {
    if (armed || !refocus.current) return;
    refocus.current = false;
    const active = document.activeElement;
    // Only reclaim focus that was lost, never take it from wherever the
    // user moved it while the action was running.
    if (!active || active === document.body) triggerRef.current?.focus();
  }, [armed]);

  function disarm() {
    refocus.current = true;
    setArmed(false);
  }

  if (!armed) {
    return (
      <button
        ref={triggerRef}
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
      disarm();
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
        onClick={disarm}
      >
        Cancel
      </button>
    </span>
  );
}
