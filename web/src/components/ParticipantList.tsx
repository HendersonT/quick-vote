import type { Participant, Phase } from "../types";

interface ParticipantListProps {
  participants: Participant[];
  phase: Phase;
}

/** Per-phase status label: suggested / voted / wants re-vote. */
function statusLabel(p: Participant, phase: Phase): string {
  if (phase === "suggesting") return p.hasSuggested ? "suggested" : "waiting";
  if (phase === "voting") return p.hasVoted ? "voted" : "waiting";
  return p.wantsRevote ? "wants re-vote" : "";
}

function isDone(p: Participant, phase: Phase): boolean {
  if (phase === "suggesting") return p.hasSuggested;
  if (phase === "voting") return p.hasVoted;
  return p.wantsRevote;
}

export default function ParticipantList({ participants, phase }: ParticipantListProps) {
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
          </li>
        ))}
      </ul>
    </aside>
  );
}
