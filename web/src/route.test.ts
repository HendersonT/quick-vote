import { describe, expect, it } from "vitest";
import { parseRoute } from "./App";

describe("parseRoute", () => {
  it("routes results pages", () => {
    expect(parseRoute("/v/AbC123xyZ9/results")).toEqual({ name: "results", slug: "AbC123xyZ9" });
    expect(parseRoute("/v/AbC123xyZ9/results/")).toEqual({ name: "results", slug: "AbC123xyZ9" });
  });
  it("keeps room and home routes", () => {
    expect(parseRoute("/v/abc")).toEqual({ name: "room", slug: "abc" });
    expect(parseRoute("/")).toEqual({ name: "home" });
    expect(parseRoute("/v/abc/other")).toEqual({ name: "not-found" });
  });
});
