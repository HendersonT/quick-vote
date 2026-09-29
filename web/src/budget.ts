// Mirrors internal/domain/ballot.go's BallotCost/ValidateBallot exactly —
// Go and TS must agree on the cost formula (see the "advanced options" spec,
// F3/F4). Defaults (exponent 2, vetoCost 0) reproduce the original
// v*v-quadratic, no-veto behavior for callers that don't pass them.

/**
 * Credit cost of putting v (>0) votes on a single option, given the vote's
 * scaling exponent. The 1e-9 epsilon keeps exact integer powers (e.g. 3^2 ==
 * 9) from rounding up to the next integer due to floating-point error in
 * Math.pow — matches Go's voteCost bit-for-bit.
 */
export const voteCost = (v: number, exponent: number): number =>
  Math.ceil(Math.pow(v, exponent) - 1e-9);

/**
 * Total credit cost of a ballot: sum over v > 0 of voteCost(v, exponent),
 * plus vetoCost for each explicit veto (encoded as the sentinel value -1).
 * Any other negative value is invalid and never reaches here in practice —
 * ValidateBallot server-side rejects it — but is ignored (costs nothing)
 * rather than throwing, since this is UI-side budget math, not validation.
 */
export const ballotCost = (
  votes: Record<string, number>,
  exponent = 2,
  vetoCost = 0,
): number =>
  Object.values(votes).reduce((s, v) => {
    if (v > 0) return s + voteCost(v, exponent);
    if (v === -1) return s + vetoCost;
    return s;
  }, 0);

export const remaining = (
  votes: Record<string, number>,
  budget: number,
  exponent = 2,
  vetoCost = 0,
) => budget - ballotCost(votes, exponent, vetoCost);

/** Max total votes reachable on `optionId` given the rest of the ballot. */
export const canIncrement = (
  votes: Record<string, number>,
  optionId: string,
  budget: number,
  exponent = 2,
  vetoCost = 0,
) => {
  const current = votes[optionId] ?? 0;
  // A vetoed option (-1) starts back at 0 votes if the user increments
  // instead of un-vetoing — incrementing is only ever offered in the UI on
  // non-vetoed rows, but guard here too so the math stays sound either way.
  const next = { ...votes, [optionId]: (current > 0 ? current : 0) + 1 };
  return ballotCost(next, exponent, vetoCost) <= budget;
};
