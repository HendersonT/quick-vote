import { describe, expect, it } from "vitest";
import { ballotCost, canIncrement, remaining, voteCost } from "./budget";

describe("ballotCost", () => {
  it("sums squared votes (default exponent 2)", () => {
    expect(ballotCost({ a: 2, b: 1 })).toBe(5);
  });

  it("is zero for an empty ballot", () => {
    expect(ballotCost({})).toBe(0);
  });

  it("applies a linear exponent of 1", () => {
    expect(ballotCost({ a: 3, b: 2 }, 1)).toBe(5);
  });

  it("applies a fractional exponent of 1.5 with epsilon rounding", () => {
    // voteCost(2, 1.5) = ceil(2^1.5 - 1e-9) = ceil(2.828... ) = 3
    // voteCost(4, 1.5) = ceil(4^1.5 - 1e-9) = ceil(8 - 1e-9) = 8 (exact power
    // stays 8, doesn't round up to 9 thanks to the epsilon)
    expect(voteCost(2, 1.5)).toBe(3);
    expect(voteCost(4, 1.5)).toBe(8);
    expect(ballotCost({ a: 4 }, 1.5)).toBe(8);
  });

  it("doesn't let an exact quadratic power round up (epsilon behavior)", () => {
    expect(voteCost(3, 2)).toBe(9);
  });

  it("costs vetoCost flat per veto (-1) entry, independent of exponent", () => {
    expect(ballotCost({ a: -1 }, 2, 5)).toBe(5);
    expect(ballotCost({ a: -1, b: -1 }, 1, 4)).toBe(8);
  });

  it("mixes positive votes and vetoes in one ballot", () => {
    // a: 2 votes @ exponent 2 = 4, b: veto @ vetoCost 3, c: 1 vote = 1
    expect(ballotCost({ a: 2, b: -1, c: 1 }, 2, 3)).toBe(8);
  });
});

describe("remaining", () => {
  it("subtracts cost from budget", () => {
    expect(remaining({ a: 2, b: 1 }, 9)).toBe(4);
  });

  it("accounts for veto costs", () => {
    expect(remaining({ a: -1 }, 10, 2, 4)).toBe(6);
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

  it("respects a linear exponent (cost == v, not v*v)", () => {
    // a:3 -> a:4 costs 1 more credit under exponent 1; budget 4 fits exactly.
    expect(canIncrement({ a: 3 }, "a", 4, 1)).toBe(true);
  });

  it("leaves room for other options' veto costs", () => {
    // b is vetoed at cost 3; only 1 credit left of a budget of 4, so a: 0->1
    // (cost 1) still fits.
    expect(canIncrement({ b: -1 }, "a", 4, 2, 3)).toBe(true);
    // but a: 1->2 (cost 4) would blow the remaining budget of 1.
    expect(canIncrement({ a: 1, b: -1 }, "a", 4, 2, 3)).toBe(false);
  });
});
