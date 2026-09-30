import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

function memStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

function renderAt(pathname: string): string {
  vi.stubGlobal("window", {
    location: { pathname, origin: "https://vote.example" },
    addEventListener: () => {},
    removeEventListener: () => {},
  });
  return renderToStaticMarkup(<App />);
}

const footers = (html: string) => html.match(/class="feedback-footer"/g) ?? [];
const bugScreen = (html: string) => {
  const href = html.match(/href="([^"]*bug_report\.yml[^"]*)"/)![1].replace(/&amp;/g, "&");
  return new URL(href).searchParams.get("screen");
};

beforeEach(() => {
  vi.stubGlobal("localStorage", memStorage());
  vi.stubGlobal("sessionStorage", memStorage());
});
afterEach(() => vi.unstubAllGlobals());

// One footer per route, with the screen the bug form should show. A room
// renders "Other" until it has joined and loaded (Room reports its phase from
// an effect, which a static render never runs).
describe("App feedback footer", () => {
  it.each([
    ["/", "Home"],
    ["/v/AbC123xyZ9", "Other"],
    ["/v/AbC123xyZ9/results", "Results page"],
    ["/nope", "Other"],
  ])("%s renders one footer with screen %s", (path, screen) => {
    const html = renderAt(path);
    expect(footers(html)).toHaveLength(1);
    expect(bugScreen(html)).toBe(screen);
    expect(html).not.toMatch(/href="[^"]*AbC123xyZ9/);
  });
});
