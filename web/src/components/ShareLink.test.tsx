import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RoomState } from "../types";
import NextVoteForm from "./NextVoteForm";
import ShareLink from "./ShareLink";

afterEach(() => vi.unstubAllGlobals());

describe("disclosure toggles", () => {
  // aria-controls ties each toggle to the region it shows and hides.
  it("the QR toggle names the panel it controls", () => {
    vi.stubGlobal("window", { location: { origin: "http://localhost" } });
    const html = renderToStaticMarkup(<ShareLink slug="abc" />);
    expect(html).toMatch(/<button[^>]*aria-expanded="false"[^>]*aria-controls="[^"]+"[^>]*>Show QR/);
  });

  it("the next-vote form carries the id its toggle points at", () => {
    const state = { settings: {} } as unknown as RoomState;
    const html = renderToStaticMarkup(
      <NextVoteForm
        id="next-form"
        slug="abc"
        sessionToken="s"
        creatorToken="c"
        name="Alice"
        state={state}
        onCancel={() => {}}
      />,
    );
    expect(html).toMatch(/^<form[^>]*id="next-form"/);
  });
});
