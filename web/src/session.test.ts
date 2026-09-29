import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  addToHistory,
  applyMove,
  getHistory,
  getSession,
  markMoved,
  recordMove,
  setHistoryClosed,
  shouldAutoMove,
  sortHistory,
  unmarkMoved,
} from "./session";
import type { RoomState } from "./types";

function memStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

const moved = (over: Partial<RoomState> = {}) =>
  ({
    slug: "old",
    title: "Old",
    next: { slug: "new", title: "New" },
    you: { participantId: "p", isCreator: false, ballot: null, nextSessionToken: "tok2" },
    ...over,
  }) as unknown as RoomState;

afterEach(() => vi.unstubAllGlobals());

describe("next-vote handoff", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  it("auto-moves once, then never again for the same old vote", () => {
    expect(shouldAutoMove("old", moved())).toBe(true);
    expect(applyMove("old", moved(), "Bob")).toBe("new");
    expect(getSession("new")?.sessionToken).toBe("tok2");
    expect(getSession("new")?.name).toBe("Bob");
    expect(shouldAutoMove("old", moved())).toBe(false);
  });

  it("does not move spectators or when there is no successor", () => {
    expect(shouldAutoMove("old", moved({ you: null } as Partial<RoomState>))).toBe(false);
    expect(shouldAutoMove("old", moved({ next: null } as Partial<RoomState>))).toBe(false);
  });

  it("does not move a participant who has no handoff token", () => {
    const st = moved({
      you: { participantId: "p", isCreator: false, ballot: null },
    } as Partial<RoomState>);
    expect(shouldAutoMove("old", st)).toBe(false);
  });

  it("carries the creator token for the creator", () => {
    const st = moved({
      you: {
        participantId: "p",
        isCreator: true,
        ballot: null,
        nextSessionToken: "t",
        nextCreatorToken: "c",
      },
    } as Partial<RoomState>);
    applyMove("old", st, "Alice");
    expect(getSession("new")?.creatorToken).toBe("c");
  });

  it("records the successor vote in history", () => {
    applyMove("old", moved(), "Bob");
    expect(getHistory()[0]).toMatchObject({ slug: "new", title: "New" });
  });
});

describe("creator starting the next vote", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  // The server broadcasts the handoff to the old room, including the
  // creator's own socket, before the create request resolves; marking the
  // old vote moved first keeps that snapshot from moving the tab a second time.
  it("ignores the old room's handoff snapshot once marked moved", () => {
    markMoved("old");
    expect(shouldAutoMove("old", moved())).toBe(false);
  });

  it("re-enables the auto-move when starting the next vote fails", () => {
    markMoved("old");
    unmarkMoved("old");
    expect(shouldAutoMove("old", moved())).toBe(true);
  });

  it("records the move with the creator's new tokens", () => {
    recordMove("old", { slug: "new", title: "New" }, {
      sessionToken: "s2",
      creatorToken: "c2",
      name: "Alice",
    });
    expect(getSession("new")).toEqual({ sessionToken: "s2", creatorToken: "c2", name: "Alice" });
    expect(getHistory()[0]).toMatchObject({ slug: "new", title: "New" });
    expect(shouldAutoMove("old", moved())).toBe(false);
  });
});

describe("history closed flag", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  it("sorts open votes before closed ones", () => {
    addToHistory("a", "A");
    addToHistory("b", "B");
    setHistoryClosed("b", true);
    expect(sortHistory(getHistory()).map((e) => e.slug)).toEqual(["a", "b"]);
  });

  it("keeps most-recent-first order within open and within closed", () => {
    addToHistory("a", "A");
    addToHistory("b", "B");
    addToHistory("c", "C");
    addToHistory("d", "D");
    setHistoryClosed("a", true);
    setHistoryClosed("c", true);
    expect(sortHistory(getHistory()).map((e) => e.slug)).toEqual(["d", "b", "c", "a"]);
  });

  it("records the flag without reordering, and reopening clears it", () => {
    addToHistory("a", "A");
    addToHistory("b", "B");
    setHistoryClosed("a", true);
    expect(getHistory().map((e) => e.slug)).toEqual(["b", "a"]);
    expect(getHistory()[1].closed).toBe(true);
    setHistoryClosed("a", false);
    expect(getHistory()[1].closed).toBe(false);
  });

  it("ignores slugs that are not in history", () => {
    addToHistory("a", "A");
    setHistoryClosed("zzz", true);
    expect(getHistory().map((e) => e.slug)).toEqual(["a"]);
  });
});

describe("history bump", () => {
  beforeEach(() => vi.stubGlobal("localStorage", memStorage()));

  it("keeps the closed flag when an entry is bumped", () => {
    addToHistory("a", "A");
    setHistoryClosed("a", true);
    addToHistory("a", "A");
    expect(getHistory()[0].closed).toBe(true);
  });
});
