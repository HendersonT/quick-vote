import type { Participant, Phase } from "../types";
import ConfirmButton from "./ConfirmButton";

interface ParticipantListProps {
  participants: Participant[];
  phase: Phase;
  /** True for the creator of an open vote: shows a remove control per row. */
  canRemove?: boolean;
  onRemove?: (participantId: string) => Promise<void> | void;
}

/** Per-phase status label: done suggesting / voted / wants re-vote. */
function statusLabel(p: Participant, phase: Phase): string {
  if (phase === "suggesting") return p.doneSuggesting ? "done" : "suggesting";
  if (phase === "voting") return p.hasVoted ? "voted" : "waiting";
  return p.wantsRevote ? "wants re-vote" : "";
}

// A participant is "done" in the suggesting phase iff they've explicitly
// marked doneSuggesting — not merely having submitted a suggestion (F1).
function isDone(p: Participant, phase: Phase): boolean {
  if (phase === "suggesting") return p.doneSuggesting;
  if (phase === "voting") return p.hasVoted;
  return p.wantsRevote;
}

export default function ParticipantList({
  participants,
  phase,
  canRemove = false,
  onRemove,
}: ParticipantListProps) {
  return (
    <aside className="participant-list" aria-label="Participants">
      <h2>Participants</h2>
      <ul>
        {participants.map((p) => (
          <li key={p.id} className="participant">
            <span
              className={`status-dot ${isDone(p, phase) ? "status-dot-done" : ""}`}
              aria-hidden="true"
            />
            <span className="participant-name">
              {p.isCreator && (
                <span className="crown" title="Creator" aria-label="Creator">
                  👑
                </span>
              )}
              {p.name}
            </span>
            <span className="participant-status">{statusLabel(p, phase)}</span>
            {canRemove && onRemove && !p.isCreator && (
              <ConfirmButton
                label="×"
                confirmLabel="Remove"
                className="remove-participant"
                ariaLabel={`Remove ${p.name}`}
                onConfirm={() => onRemove(p.id)}
              />
            )}
          </li>
        ))}
      </ul>
    </aside>
  );
}
