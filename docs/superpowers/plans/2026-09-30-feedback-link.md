# Feedback Link Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every page shows "Report a bug · Suggest a feature", opening prefilled GitHub issue forms with safe context only.

**Architecture:** GitHub issue forms in `.github/ISSUE_TEMPLATE/`; a pure link builder (`web/src/feedback.ts`) that by construction never sees the URL or vote; one footer rendered once in `App.tsx`, with `Room` reporting its phase upward via a callback. Version and destination are Vite build-time env vars fed from Docker build args.

**Tech Stack:** React 18 + TypeScript, Vite 8 / Vitest 5, GitHub issue forms (YAML), Docker.

**Spec:** `docs/superpowers/specs/2026-09-30-feedback-link-design.md`

## Global Constraints

- Branch `feedback-link`; never push. Commits end with both trailers:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01QdFq7Vk7R975qFWQTUXzoS`.
- Prefill safe context only: app version, current screen, browser user-agent. Never a vote link/code, participant names, or suggestion titles.
- Default destination `https://github.com/HendersonT/quick-vote/issues`; override via `VITE_ISSUES_URL` (build time). Version via `VITE_APP_VERSION`, default `"dev"`.
- No new npm dependencies. No server (Go) changes.
- Links: `target="_blank" rel="noopener noreferrer"`.
- `npm run build`, `npm test` pass; `go test -race ./...` stays green; nothing violates the CSP (no inline scripts/handlers, no external origins loaded — links to github.com are navigation, which CSP does not restrict).

## Review Focus

1. A vote code must never appear in any footer href, even when rendered from inside a room at `/v/<slug>`. (Test: Task 3 `never puts the vote code in a link`.)
2. The app's screen labels must match the bug form's dropdown options exactly, or GitHub silently ignores the prefill. (Test: Task 2 `screen labels match the bug form dropdown`.)
3. A user-agent containing `&`, `#`, `=` or spaces must not break or inject query params. (Test: Task 2 `encodes hostile user-agent characters`.)
4. An empty `VITE_ISSUES_URL` (the Docker default) must fall back to the upstream repo, not produce `"/new?..."`. (Test: Task 2 `empty base falls back to upstream`.)
5. The footer must not overlap content or overflow at 400 px wide. (Check: Task 3 Step 6 browser check.)

---

### Task 1: GitHub issue forms (spec F1)

**Files:** Create `.github/ISSUE_TEMPLATE/bug_report.yml`, `.github/ISSUE_TEMPLATE/feature_request.yml`, `.github/ISSUE_TEMPLATE/config.yml`.

**Interfaces:**
- Produces: bug form field ids `what`, `steps`, `expected`, `screen`, `version`, `browser`; `screen` dropdown options exactly `Home`, `Suggesting`, `Voting`, `Results`, `Results page`, `Other`. Feature form ids `problem`, `idea`, `alternatives`, `version`. Template filenames `bug_report.yml`, `feature_request.yml`.

- [ ] **Step 1: Write** `bug_report.yml`:

```yaml
name: Bug report
description: Something in Quick Vote didn't work the way it should.
title: "[Bug] "
labels: [bug]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for reporting! **Please don't paste vote links or codes** — anyone with a link can join that vote.
  - type: textarea
    id: what
    attributes:
      label: What happened?
    validations:
      required: true
  - type: textarea
    id: steps
    attributes:
      label: Steps to reproduce
      placeholder: "1. Create a vote…\n2. …"
  - type: textarea
    id: expected
    attributes:
      label: What did you expect?
  - type: dropdown
    id: screen
    attributes:
      label: Screen
      options:
        - Home
        - Suggesting
        - Voting
        - Results
        - Results page
        - Other
  - type: input
    id: version
    attributes:
      label: App version
  - type: input
    id: browser
    attributes:
      label: Browser / device
```

`feature_request.yml`:

```yaml
name: Feature suggestion
description: An idea to make Quick Vote better.
title: "[Idea] "
labels: [enhancement]
body:
  - type: textarea
    id: problem
    attributes:
      label: What problem would this solve?
    validations:
      required: true
  - type: textarea
    id: idea
    attributes:
      label: Your idea
    validations:
      required: true
  - type: textarea
    id: alternatives
    attributes:
      label: Alternatives you've considered
  - type: input
    id: version
    attributes:
      label: App version
```

`config.yml`:

```yaml
blank_issues_enabled: false
```

- [ ] **Step 2: Validate** — `python3 -c "import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]" .github/ISSUE_TEMPLATE/*.yml` (no output = valid). Also check each body item has a unique `id` where present: `grep -c "id:" .github/ISSUE_TEMPLATE/bug_report.yml` → 6.
- [ ] **Step 3: Commit** — `feat: GitHub issue forms for bug reports and feature suggestions`.

---

### Task 2: Link builder (spec F2)

**Files:** Create `web/src/feedback.ts`, `web/src/feedback.test.ts`, `web/src/vite-env.d.ts`.

**Interfaces:**
- Consumes: `.github/ISSUE_TEMPLATE/bug_report.yml` (Task 1) — read as text by the test.
- Produces:

```ts
export type FeedbackKind = "bug" | "feature";
export type Screen = "home" | "suggesting" | "voting" | "results" | "results-page" | "other";
export const SCREEN_LABELS: Record<Screen, string>;
export const UPSTREAM_ISSUES_URL = "https://github.com/HendersonT/quick-vote/issues";
export interface FeedbackContext { screen: Screen; version: string; userAgent: string }
export function issueUrl(kind: FeedbackKind, ctx: FeedbackContext, base?: string): string;
/** Context for the running app: version from the build, UA from navigator. */
export function currentContext(screen: Screen): FeedbackContext;
```

- [ ] **Step 1: Failing tests** `web/src/feedback.test.ts`:

```ts
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { issueUrl, SCREEN_LABELS, UPSTREAM_ISSUES_URL, type FeedbackContext } from "./feedback";

const ctx: FeedbackContext = { screen: "voting", version: "abc1234", userAgent: "Mozilla/5.0 Firefox/131.0" };
const params = (u: string) => new URL(u).searchParams;

describe("issueUrl", () => {
  it("prefills the bug form with safe context", () => {
    const u = issueUrl("bug", ctx);
    expect(u.startsWith(UPSTREAM_ISSUES_URL + "/new?")).toBe(true);
    const p = params(u);
    expect(p.get("template")).toBe("bug_report.yml");
    expect(p.get("screen")).toBe("Voting");
    expect(p.get("version")).toBe("abc1234");
    expect(p.get("browser")).toBe("Mozilla/5.0 Firefox/131.0");
  });

  it("feature form gets only the version", () => {
    const p = params(issueUrl("feature", ctx));
    expect(p.get("template")).toBe("feature_request.yml");
    expect(p.get("version")).toBe("abc1234");
    expect(p.has("screen")).toBe(false);
    expect(p.has("browser")).toBe(false);
  });

  it("encodes hostile user-agent characters", () => {
    const ua = "X&template=evil.yml#frag =1";
    const p = params(issueUrl("bug", { ...ctx, userAgent: ua }));
    expect(p.get("browser")).toBe(ua);
    expect(p.getAll("template")).toEqual(["bug_report.yml"]);
  });

  it("truncates very long user-agents to 200 characters", () => {
    const p = params(issueUrl("bug", { ...ctx, userAgent: "a".repeat(500) }));
    expect(p.get("browser")).toHaveLength(200);
  });

  it("honors a custom destination and strips a trailing slash", () => {
    expect(issueUrl("bug", ctx, "https://github.com/someone/fork/issues/")).toMatch(
      /^https:\/\/github\.com\/someone\/fork\/issues\/new\?/,
    );
  });

  it("empty base falls back to upstream", () => {
    expect(issueUrl("bug", ctx, "").startsWith(UPSTREAM_ISSUES_URL + "/new?")).toBe(true);
  });
});

describe("screen labels match the bug form dropdown", () => {
  it("every label is an option in bug_report.yml", () => {
    const yml = readFileSync(new URL("../../.github/ISSUE_TEMPLATE/bug_report.yml", import.meta.url), "utf8");
    for (const label of Object.values(SCREEN_LABELS)) {
      expect(yml).toContain(`- ${label}\n`);
    }
  });
});
```

- [ ] **Step 2: Run** `cd web && npx vitest run src/feedback.test.ts` — Expected: FAIL (module not found).
- [ ] **Step 3: Implement.** `web/src/vite-env.d.ts`:

```ts
/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Commit the image was built from (Docker build arg APP_VERSION). */
  readonly VITE_APP_VERSION?: string;
  /** Where feedback links point (Docker build arg ISSUES_URL). */
  readonly VITE_ISSUES_URL?: string;
}
```

`web/src/feedback.ts`:

```ts
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

export function currentContext(screen: Screen): FeedbackContext {
  return {
    screen,
    version: import.meta.env.VITE_APP_VERSION || "dev",
    userAgent: typeof navigator === "undefined" ? "" : navigator.userAgent,
  };
}
```

- [ ] **Step 4: Run** `npx vitest run src/feedback.test.ts` then `npm run build && npm test` — PASS.
- [ ] **Step 5: Commit** — `feat(web): build safe prefilled GitHub issue links`.

---

### Task 3: Footer on every page (spec F3)

**Files:** Create `web/src/components/FeedbackFooter.tsx`, `web/src/components/FeedbackFooter.test.tsx`. Modify `web/src/App.tsx`, `web/src/pages/Room.tsx`, `web/src/styles.css`.

**Interfaces:**
- Consumes: `issueUrl`, `currentContext`, `Screen` from `web/src/feedback.ts` (Task 2).
- Produces: `export default function FeedbackFooter({ screen }: { screen: Screen })`; `Room` gains optional prop `onScreen?: (s: Screen) => void`.

- [ ] **Step 1: Failing test** `FeedbackFooter.test.tsx`:

```tsx
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import FeedbackFooter from "./FeedbackFooter";

afterEach(() => vi.unstubAllGlobals());

const hrefs = (html: string) => [...html.matchAll(/href="([^"]+)"/g)].map((m) => m[1].replace(/&amp;/g, "&"));

describe("FeedbackFooter", () => {
  it("links to both forms in a new tab without a referrer", () => {
    const html = renderToStaticMarkup(<FeedbackFooter screen="home" />);
    expect(html).toContain("Report a bug");
    expect(html).toContain("Suggest a feature");
    const [bug, idea] = hrefs(html);
    expect(new URL(bug).searchParams.get("template")).toBe("bug_report.yml");
    expect(new URL(idea).searchParams.get("template")).toBe("feature_request.yml");
    expect(html.match(/target="_blank"/g)).toHaveLength(2);
    expect(html.match(/rel="noopener noreferrer"/g)).toHaveLength(2);
  });

  it("never puts the vote code in a link", () => {
    vi.stubGlobal("location", new URL("https://vote.example/v/Sl9gCoDe42"));
    vi.stubGlobal("navigator", { userAgent: "UA" });
    const html = renderToStaticMarkup(<FeedbackFooter screen="voting" />);
    for (const h of hrefs(html)) expect(h).not.toContain("Sl9gCoDe42");
    expect(new URL(hrefs(html)[0]).searchParams.get("screen")).toBe("Voting");
  });
});
```

- [ ] **Step 2: Run** — FAIL (module not found).
- [ ] **Step 3: Implement** `FeedbackFooter.tsx`:

```tsx
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
```

- [ ] **Step 4: Wire it up.**
  - `App.tsx`: add `const [roomScreen, setRoomScreen] = useState<Screen>("other");`. Compute the page element in the existing `switch` (assign to `page` instead of returning), passing `onScreen={setRoomScreen}` to `<Room>`. Screen for the footer: `home` → `"home"`, `room` → `roomScreen`, `results` → `"results-page"`, otherwise `"other"`. Return `<>{page}<FeedbackFooter screen={screen} /></>`.
  - `Room.tsx`: accept `onScreen?: (s: Screen) => void`; add `useEffect(() => { onScreen?.(state && session ? state.phase : "other"); }, [onScreen, state?.phase, session]);` (phase values `suggesting`/`voting`/`results` are valid `Screen` values). Because Room is keyed by slug, the effect runs on every room mount, so a stale phase never carries across votes.
  - `styles.css`: `.feedback-footer { margin: 2rem auto 1rem; padding: 0 1rem; text-align: center; font-size: 0.85rem; color: var(--muted, #6b7280); }` and `.feedback-footer a { color: inherit; }` — use the stylesheet's existing muted-text custom property if one exists (grep `--` in `:root`), in both light and dark blocks.
- [ ] **Step 5: Run** `npm run build && npm test` — PASS.
- [ ] **Step 6: Browser check.** Build the Go binary with the new UI embedded (see README "Rebuilding the embedded frontend"; restore `webembed/dist` afterwards), run on `127.0.0.1:18096` with a temp DB, and with headless Chromium (Playwright 1.62 from `/tmp/claude-1000/e2e/node_modules`) at 400×800 and 1200×800: footer visible on home, in a room (screen param = phase), results page, and not-found; no horizontal scroll (`document.documentElement.scrollWidth <= innerWidth`); no console errors; hrefs contain no slug.
- [ ] **Step 7: Commit** — `feat(web): feedback footer on every page`.

---

### Task 4: Build plumbing and docs (spec F4)

**Files:** Modify `Dockerfile`, `README.md`.

- [ ] **Step 1:** In the `web` stage of `Dockerfile`, before `RUN npm run build`:

```dockerfile
# Stamped into the UI for feedback links; deploys pass the commit hash.
ARG APP_VERSION=dev
# Where feedback links point; empty means the upstream repo.
ARG ISSUES_URL=
ENV VITE_APP_VERSION=$APP_VERSION VITE_ISSUES_URL=$ISSUES_URL
```

- [ ] **Step 2: Verify** — `docker build -q -t qv:fb --build-arg APP_VERSION=test123 .` then run it on `127.0.0.1:18097` with `--tmpfs /data:uid=10001` and `curl -s http://127.0.0.1:18097/ | grep -o 'assets/index-[^"]*js'` → fetch that asset and `grep -c test123` ≥ 1. Stop the container.
- [ ] **Step 3: README.** Add a `## Feedback` section (after "How it works"): bugs and ideas go to GitHub issues via the in-app footer or the repo's issue forms; please don't post vote links. In `### Configuration` add a "Build arguments" table: `APP_VERSION` (default `dev`, shown in feedback reports; deploy with `--build-arg APP_VERSION=$(git rev-parse --short HEAD)`), `ISSUES_URL` (default this repo's issues; self-hosters can point it at their own).
- [ ] **Step 4: Run** `npm run build && npm test` and `go test -race ./...` — PASS.
- [ ] **Step 5: Commit** — `build: stamp app version and feedback destination into the UI`.

---

## Controller steps (after Task 4)

1. Review the whole branch diff; run all checks.
2. Merge `feedback-link` → `master`, push (gh credential helper), confirm CI green; GitHub should now show the two forms at `…/issues/new/choose`.
3. Deploy: DB snapshot via `ops/backup.sh`; `git fetch && git reset --hard origin/master` in `/opt/services/vote/app`; `docker compose build --build-arg APP_VERSION=$(git -C app rev-parse --short HEAD) vote-app` (no pipe masking the exit status); `docker compose up -d vote-app`.
4. Live check: footer present; open the bug link and confirm GitHub renders the prefilled form with the deployed commit as version.
