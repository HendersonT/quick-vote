import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getVote } from "./api";

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

describe("request errors", () => {
  it("carry the HTTP status and the server's message", async () => {
    stubFetch(404, { error: "vote not found" });
    const err = await getVote("gone").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(404);
    expect((err as ApiError).message).toBe("vote not found");
  });

  it("fall back to a generic message when the body isn't JSON", async () => {
    vi.stubGlobal("fetch", async () => new Response("bad gateway", { status: 502 }));
    const err = await getVote("abc").catch((e: unknown) => e);
    expect((err as ApiError).status).toBe(502);
    expect((err as ApiError).message).toBe("request failed: 502");
  });
});
