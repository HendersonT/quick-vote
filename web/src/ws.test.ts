import { describe, expect, it } from "vitest";
import { authMessage } from "./ws";

describe("authMessage", () => {
  it("sends only the session token without a creator token", () => {
    expect(JSON.parse(authMessage("s1"))).toEqual({ type: "auth", token: "s1" });
    expect(JSON.parse(authMessage(""))).toEqual({ type: "auth", token: "" });
  });

  it("adds the creator token when the session has one", () => {
    expect(JSON.parse(authMessage("s1", "c1"))).toEqual({
      type: "auth",
      token: "s1",
      creatorToken: "c1",
    });
  });
});
