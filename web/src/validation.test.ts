import { describe, expect, it } from "vitest";
import { checkName, checkTitle } from "./validation";

describe("checkTitle", () => {
  it("trims and accepts a normal title", () => {
    expect(checkTitle("  Friday game night ")).toEqual({ ok: true, value: "Friday game night" });
  });

  it("rejects an empty or blank title", () => {
    expect(checkTitle("   ")).toEqual({ ok: false, error: "Give the vote a title." });
  });

  it("rejects more than 200 characters", () => {
    expect(checkTitle("a".repeat(200)).ok).toBe(true);
    expect(checkTitle("a".repeat(201))).toEqual({
      ok: false,
      error: "Title must be 200 characters or fewer.",
    });
  });

  it("counts characters the way the server does, not UTF-16 units", () => {
    // 200 emoji are 400 UTF-16 code units but 200 characters (runes).
    expect(checkTitle("🎲".repeat(200)).ok).toBe(true);
  });
});

describe("checkName", () => {
  it("rejects a blank name", () => {
    expect(checkName(" ")).toEqual({ ok: false, error: "Enter your name." });
  });

  it("rejects more than 50 characters", () => {
    expect(checkName("é".repeat(50))).toEqual({ ok: true, value: "é".repeat(50) });
    expect(checkName("b".repeat(51))).toEqual({
      ok: false,
      error: "Name must be 50 characters or fewer.",
    });
  });
});
