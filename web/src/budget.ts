export const ballotCost = (votes: Record<string, number>) =>
  Object.values(votes).reduce((s, v) => s + v * v, 0);

export const remaining = (votes: Record<string, number>, budget: number) =>
  budget - ballotCost(votes);

/** Max total votes reachable on `optionId` given the rest of the ballot. */
export const canIncrement = (
  votes: Record<string, number>,
  optionId: string,
  budget: number,
) => {
  const next = { ...votes, [optionId]: (votes[optionId] ?? 0) + 1 };
  return ballotCost(next) <= budget;
};
