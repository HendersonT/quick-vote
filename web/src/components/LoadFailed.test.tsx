import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ApiError } from "../api";
import LoadFailed from "./LoadFailed";

const render = (error: unknown) =>
  renderToStaticMarkup(<LoadFailed error={error} what="results" />).replace(/&#x27;/g, "'");

describe("LoadFailed", () => {
  it("says the vote wasn't found only for a 404", () => {
    expect(render(new ApiError(404, "vote not found"))).toContain("Vote not found");
  });

  it("shows a generic failure for anything else", () => {
    for (const err of [new ApiError(500, "internal error"), new TypeError("Failed to fetch")]) {
      const html = render(err);
      expect(html).not.toContain("not found");
      expect(html).toContain("Couldn't load results");
    }
  });
});
