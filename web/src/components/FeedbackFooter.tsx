import { currentContext, issueUrl, type Screen } from "../feedback";

/** Site-wide "report a bug / suggest a feature" links (GitHub issue forms). */
export default function FeedbackFooter({ screen }: { screen: Screen }) {
  const ctx = currentContext(screen);
  return (
    <footer className="feedback-footer">
      <a href={issueUrl("bug", ctx)} target="_blank" rel="noopener noreferrer">
        Report a bug
      </a>
      <span aria-hidden="true"> · </span>
      <a href={issueUrl("feature", ctx)} target="_blank" rel="noopener noreferrer">
        Suggest a feature
      </a>
    </footer>
  );
}
