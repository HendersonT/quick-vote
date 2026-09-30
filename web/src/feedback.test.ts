import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { issueUrl, SCREEN_LABELS, UPSTREAM_ISSUES_URL, type FeedbackContext } from "./feedback";

const ctx: FeedbackContext = { screen: "voting", version: "abc1234", userAgent: "Mozilla/5.0 Firefox/131.0" };
const params = (u: string) => new URL(u).searchParams;

// Hermetic: a VITE_ISSUES_URL exported in the shell (e.g. by a self-hoster)
// must not change what these tests expect.
beforeEach(() => vi.stubEnv("VITE_ISSUES_URL", ""));
afterEach(() => vi.unstubAllEnvs());

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

  it("encodes hostile version strings", () => {
    const version = "v1 &x=#y";
    expect(params(issueUrl("bug", { ...ctx, version })).get("version")).toBe(version);
    expect(params(issueUrl("feature", { ...ctx, version })).get("version")).toBe(version);
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

describe("bug form prefill fields", () => {
  const yml = readFileSync(new URL("../../.github/ISSUE_TEMPLATE/bug_report.yml", import.meta.url), "utf8");

  // GitHub only prefills text fields from the URL (not dropdowns), so every
  // prefilled id must be an input or textarea in the form.
  it("screen, version and browser are text inputs", () => {
    for (const id of ["screen", "version", "browser"]) {
      expect(yml).toMatch(new RegExp(`- type: input\\n    id: ${id}\\n`));
    }
  });

  it("the screen field's hint lists every label the app sends", () => {
    const screenField = yml.slice(yml.indexOf("id: screen"), yml.indexOf("id: version"));
    for (const label of Object.values(SCREEN_LABELS)) {
      expect(screenField).toContain(label);
    }
  });
});
