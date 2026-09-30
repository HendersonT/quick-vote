// Builds "report a bug" / "suggest a feature" links to GitHub issue forms.
// Only safe context is prefilled — version, screen, browser — and this module
// never receives the page URL or room state, so a vote's link (its only
// access control) can't end up in a public issue.

export type FeedbackKind = "bug" | "feature";
export type Screen = "home" | "suggesting" | "voting" | "results" | "results-page" | "other";

/** Must match the "screen" dropdown options in .github/ISSUE_TEMPLATE/bug_report.yml. */
export const SCREEN_LABELS: Record<Screen, string> = {
  home: "Home",
  suggesting: "Suggesting",
  voting: "Voting",
  results: "Results",
  "results-page": "Results page",
  other: "Other",
};

export const UPSTREAM_ISSUES_URL = "https://github.com/HendersonT/quick-vote/issues";

export interface FeedbackContext {
  screen: Screen;
  version: string;
  userAgent: string;
}

const MAX_UA = 200;

export function issueUrl(
  kind: FeedbackKind,
  ctx: FeedbackContext,
  base: string = import.meta.env.VITE_ISSUES_URL ?? "",
): string {
  const root = (base.trim() || UPSTREAM_ISSUES_URL).replace(/\/+$/, "");
  const params = new URLSearchParams();
  if (kind === "bug") {
    params.set("template", "bug_report.yml");
    params.set("screen", SCREEN_LABELS[ctx.screen]);
    params.set("version", ctx.version);
    params.set("browser", ctx.userAgent.slice(0, MAX_UA));
  } else {
    params.set("template", "feature_request.yml");
    params.set("version", ctx.version);
  }
  return `${root}/new?${params.toString()}`;
}

/** Context for the running app: version from the build, UA from navigator. */
export function currentContext(screen: Screen): FeedbackContext {
  return {
    screen,
    version: import.meta.env.VITE_APP_VERSION || "dev",
    userAgent: typeof navigator === "undefined" ? "" : navigator.userAgent,
  };
}
