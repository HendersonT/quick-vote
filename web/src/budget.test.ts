import { describe, expect, it } from "vitest";
import { ballotCost, canIncrement, remaining } from "./budget";

describe("ballotCost", () => {
  it("sums squared votes", () => {
    expect(ballotCost({ a: 2, b: 1 })).toBe(5);
  });

  it("is zero for an empty ballot", () => {
    expect(ballotCost({})).toBe(0);
  });
});

describe("remaining", () => {
  it("subtracts cost from budget", () => {
    expect(remaining({ a: 2, b: 1 }, 9)).toBe(4);
  });
});

describe("canIncrement", () => {
  it("allows 0 -> 1 when budget covers the cost", () => {
    expect(canIncrement({}, "a", 1)).toBe(true);
  });

  it("disallows 1 -> 2 when budget is exceeded (cost 4 > budget 3)", () => {
    expect(canIncrement({ a: 1 }, "a", 3)).toBe(false);
  });

  it("allows exactly hitting the budget boundary", () => {
    // a:1 -> a:2 costs 4; budget of 4 should just fit.
    expect(canIncrement({ a: 1 }, "a", 4)).toBe(true);
  });
});
