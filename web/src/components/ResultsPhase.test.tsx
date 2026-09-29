import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { RoomState } from "../types";
import ResultsPhase from "./ResultsPhase";

const state = {
  slug: "abc",
  title: "Game night",
  phase: "results",
  settings: { survivalThreshold: 2 },
  participants: [],
  options: [
    { id: "a", title: "Catan" },
    { id: "b", title: "Azul" },
  ],
  you: null,
  results: {
    scores: [
      { optionId: "a", score: 7, backers: 3, vetoCount: 0, eliminated: false },
      { optionId: "b", score: 1, backers: 1, vetoCount: 0, eliminated: true },
    ],
    winnerOptionId: "a",
    tiePending: false,
    tiedOptionIds: [],
    tiebreakNote: "",
    revoteCalls: 0,
    revoteNeeded: 1,
  },
} as unknown as RoomState;

describe("ResultsPhase", () => {
  // A score is the total of the votes cast on an option; credits are the
  // budget those votes cost, so they are the wrong unit here.
  it("labels scores and the survival threshold in votes", () => {
    const html = renderToStaticMarkup(
      <ResultsPhase readOnly slug="abc" sessionToken="" state={state} />,
    );
    expect(html).toContain("7 votes · 3 backers");
    expect(html).toContain("1 vote · 1 backer");
    expect(html).toContain("eliminated — under 2 votes");
    expect(html).not.toContain("credit");
  });
});
