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
