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
