import { afterEach, describe, expect, it, vi } from "vitest";
import { getVote } from "./api";

function stubFetch(status: number, body: unknown) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify(body), { status }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function sentHeaders(fetchMock: ReturnType<typeof stubFetch>): Record<string, string> {
  return (fetchMock.mock.calls[0][1]?.headers ?? {}) as Record<string, string>;
}

afterEach(() => vi.unstubAllGlobals());

describe("getVote", () => {
  it("sends no credentials for a spectator", async () => {
    const f = stubFetch(200, {});
    await getVote("abc");
    expect(sentHeaders(f).Authorization).toBeUndefined();
    expect(sentHeaders(f)["X-Creator-Token"]).toBeUndefined();
  });

  it("sends the creator token alongside the session", async () => {
    const f = stubFetch(200, {});
    await getVote("abc", "s1", "c1");
    expect(sentHeaders(f).Authorization).toBe("Bearer s1");
    expect(sentHeaders(f)["X-Creator-Token"]).toBe("c1");
  });
});
