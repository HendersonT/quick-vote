# Quick Vote — bug report & feature suggestion links

Date: 2026-09-30. Status: approved in conversation, pending written-spec review.

## Purpose

Give people using Quick Vote (the public demo and self-hosted copies) an
obvious way to report a bug or suggest a feature, landing as trackable
GitHub issues on `HendersonT/quick-vote`, without adding any backend, storage,
or spam surface to the server.

**Success =** every page shows "Report a bug · Suggest a feature"; each opens
the matching GitHub issue form with safe context prefilled; nothing that
grants access to a vote can end up in the prefilled issue; CI green; deployed
and the live links open correctly prefilled forms.

**Decisions (from conversation):** destination is GitHub Issues (no in-app
inbox); prefill **safe context only** — app version, current screen, browser
user-agent. Never the vote link/code, participant names, or suggestion
titles.

## F1. Issue forms (repo)

- `.github/ISSUE_TEMPLATE/bug_report.yml` — name "Bug report", label `bug`,
  title prefix `[Bug] `. Fields (ids in backticks are the prefill keys):
  - markdown notice: "Please don't paste vote links or codes — anyone with a
    link can join that vote."
  - `what` textarea (required): What happened?
  - `steps` textarea: Steps to reproduce
  - `expected` textarea: What did you expect?
  - `screen` input, with a hint listing Home, Suggesting, Voting, Results,
    Results page, Other (an input, not a dropdown: GitHub only prefills text
    fields from the URL)
  - `version` input: App version
  - `browser` input: Browser / device
- `.github/ISSUE_TEMPLATE/feature_request.yml` — name "Feature suggestion",
  label `enhancement`, title prefix `[Idea] `. Fields: `problem` textarea
  (required): What problem would this solve?; `idea` textarea (required): Your
  idea; `alternatives` textarea: Alternatives you've considered; `version`
  input.
- `.github/ISSUE_TEMPLATE/config.yml` — `blank_issues_enabled: false`.
- Labels `bug` and `enhancement` already exist on the repo.

## F2. Link builder (`web/src/feedback.ts`)

```ts
export type FeedbackKind = "bug" | "feature";
export type Screen = "home" | "suggesting" | "voting" | "results" | "results-page" | "other";
export function issueUrl(kind: FeedbackKind, ctx: { screen: Screen; version: string; userAgent: string }): string
```

- Base: `import.meta.env.VITE_ISSUES_URL` (build-time), default
  `https://github.com/HendersonT/quick-vote/issues`. Result:
  `<base>/new?template=bug_report.yml&screen=<Label>&version=<v>&browser=<ua>`
  (feature: `template=feature_request.yml&version=<v>`), values
  `URLSearchParams`-encoded. `screen` values are the labels listed in the
  form's hint.
- The function takes no URL, slug, state, or names — the vote code cannot
  reach it by construction. `userAgent` is truncated to 200 characters.
- Version: `import.meta.env.VITE_APP_VERSION`, default `"dev"`.

## F3. Footer (every page)

- `web/src/components/FeedbackFooter.tsx`: `<footer>` with two links, "Report a
  bug" and "Suggest a feature", `target="_blank" rel="noopener noreferrer"`.
  Props: `screen: Screen`. Rendered by Home (`home`), Room (from phase:
  `suggesting`/`voting`/`results`; `other` before join/loading), ResultsView
  (`results-page`), and the not-found page (`other`). Styled small and muted
  at the bottom, light and dark themes, readable at 400 px width.
- The site's `Referrer-Policy: no-referrer` already keeps the room URL out of
  GitHub's Referer; `noreferrer` on the link is belt-and-braces.

## F4. Build plumbing

- `Dockerfile` web stage: `ARG APP_VERSION=dev`, `ARG ISSUES_URL=`, exported
  as `VITE_APP_VERSION` / `VITE_ISSUES_URL` for `npm run build` (empty
  `ISSUES_URL` → default).
- Deploy passes `--build-arg APP_VERSION=$(git -C app rev-parse --short HEAD)`.
  CI builds without it (`dev`).
- README: "Feedback" section (links to the issue forms) and the two build
  args in the configuration docs, noting self-hosters can point reports at
  their own repo.

## Testing

- Vitest `feedback.test.ts`: bug URL has the right template and each prefill
  param, correctly encoded (spaces, `&`, `#` in UA/version); feature URL uses
  the feature template and omits screen/browser; `VITE_ISSUES_URL` override is
  honored (test via an injectable base parameter or `vi.stubEnv`); UA
  truncation; the prefilled ids are text inputs in `bug_report.yml` and the
  screen hint lists every label (test reads the YAML text).
- Footer render test (react-dom/server static render): both hrefs present
  with `target=_blank` and `rel` containing `noreferrer`; rendering the Room
  footer for a room path never includes the slug in any href.
- Go tests unaffected; `go test -race ./...` and web build/test stay green.
- After deploy: open the live footer link and confirm GitHub shows the
  prefilled bug form (version = deployed commit).

## Non-goals

In-app inbox or storage; notifications; screenshots/attachments; prefilling
vote links, names, or suggestion titles; GitHub Discussions.
