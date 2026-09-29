import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import JoinGate from "./JoinGate";

// Static markup escapes apostrophes; unescape them so assertions read as UI text.
const render = (props: { closed?: boolean; removed?: boolean }) =>
  renderToStaticMarkup(
    <JoinGate slug="abc" title="Game night" onJoined={() => {}} {...props} />,
  ).replace(/&#x27;/g, "'");

describe("JoinGate", () => {
  it("invites an open vote's visitors to join", () => {
    const html = render({});
    expect(html).toContain("Enter your name to join.");
    expect(html).not.toMatch(/<input[^>]*disabled/);
    expect(html).not.toMatch(/<button[^>]*disabled/);
  });

  it("turns joining off on a closed vote", () => {
    const html = render({ closed: true });
    expect(html).not.toContain("Enter your name to join.");
    expect(html).toContain("New participants can't join while the vote is closed.");
    expect(html).toMatch(/<input[^>]*id="joinName"[^>]*disabled/);
    expect(html).toMatch(/<button[^>]*type="submit"[^>]*disabled/);
  });

  it("tells a removed participant they can rejoin only while the vote is open", () => {
    expect(render({ removed: true })).toContain(
      "You're no longer in this vote — you can join again below.",
    );
    const closedHtml = render({ removed: true, closed: true });
    expect(closedHtml).toContain("You're no longer in this vote.");
    expect(closedHtml).not.toContain("join again");
  });
});
