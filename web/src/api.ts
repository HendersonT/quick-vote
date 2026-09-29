import type { RoomState, Settings } from "./types";

const BASE = "/api";

interface ApiError {
  error: string;
}

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const json = await res.json().catch(() => ({}) as unknown);
  if (!res.ok) {
    const message = (json as ApiError)?.error ?? `request failed: ${res.status}`;
    throw new Error(message);
  }
  return json as T;
}

function authHeaders(sessionToken: string, creatorToken?: string): HeadersInit {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${sessionToken}`,
  };
  if (creatorToken) headers["X-Creator-Token"] = creatorToken;
  return headers;
}

export interface CreateVoteResponse {
  slug: string;
  creatorToken: string;
  sessionToken: string;
  state: RoomState;
}

export function createVote(
  title: string,
  creatorName: string,
  settings?: Partial<Settings>,
): Promise<CreateVoteResponse> {
  return request<CreateVoteResponse>("/votes", {
    method: "POST",
    body: JSON.stringify({ title, creatorName, settings }),
  });
}

export function getVote(slug: string, sessionToken?: string): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}`, {
    headers: sessionToken ? { Authorization: `Bearer ${sessionToken}` } : undefined,
  });
}

export interface JoinResponse {
  sessionToken: string;
  state: RoomState;
}

export function joinVote(slug: string, name: string): Promise<JoinResponse> {
  return request<JoinResponse>(`/votes/${slug}/join`, {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function addSuggestion(
  slug: string,
  sessionToken: string,
  title: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/suggestions`, {
    method: "POST",
    headers: authHeaders(sessionToken),
    body: JSON.stringify({ title }),
  });
}

/**
 * Deletes a suggestion. Participants may delete their own; passing the
 * creator token lets the creator delete anyone's (spec B2).
 */
export function deleteSuggestion(
  slug: string,
  sessionToken: string,
  optionId: string,
  creatorToken?: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/suggestions/${optionId}`, {
    method: "DELETE",
    headers: authHeaders(sessionToken, creatorToken),
  });
}

export function putBallot(
  slug: string,
  sessionToken: string,
  votes: Record<string, number>,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/ballot`, {
    method: "PUT",
    headers: authHeaders(sessionToken),
    body: JSON.stringify({ votes }),
  });
}

export function advanceVote(
  slug: string,
  sessionToken: string,
  creatorToken: string,
  winnerOptionId?: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/advance`, {
    method: "POST",
    headers: authHeaders(sessionToken, creatorToken),
    body: JSON.stringify(winnerOptionId ? { winnerOptionId } : {}),
  });
}

export function toggleRevote(
  slug: string,
  sessionToken: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/revote`, {
    method: "POST",
    headers: authHeaders(sessionToken),
    body: JSON.stringify({}),
  });
}

export function toggleDoneSuggesting(
  slug: string,
  sessionToken: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/done-suggesting`, {
    method: "POST",
    headers: authHeaders(sessionToken),
    body: JSON.stringify({}),
  });
}

/** Creator-only: removes a participant from the vote (spec B1). */
export function removeParticipant(
  slug: string,
  sessionToken: string,
  creatorToken: string,
  participantId: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/participants/${participantId}`, {
    method: "DELETE",
    headers: authHeaders(sessionToken, creatorToken),
  });
}

/** Creator-only: closes the vote to all writes (spec B3). Idempotent. */
export function closeVote(
  slug: string,
  sessionToken: string,
  creatorToken: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/close`, {
    method: "POST",
    headers: authHeaders(sessionToken, creatorToken),
    body: JSON.stringify({}),
  });
}

/** Creator-only: reopens a closed vote (spec B3). No timer is re-armed. */
export function reopenVote(
  slug: string,
  sessionToken: string,
  creatorToken: string,
): Promise<RoomState> {
  return request<RoomState>(`/votes/${slug}/reopen`, {
    method: "POST",
    headers: authHeaders(sessionToken, creatorToken),
    body: JSON.stringify({}),
  });
}

/**
 * Creator-only: starts a follow-up vote with the same group (spec B4).
 * Settings default server-side to this vote's; `settings` patches over them.
 * Same response shape as createVote.
 */
export function createNextVote(
  slug: string,
  sessionToken: string,
  creatorToken: string,
  title: string,
  settings?: Partial<Settings>,
): Promise<CreateVoteResponse> {
  return request<CreateVoteResponse>(`/votes/${slug}/next`, {
    method: "POST",
    headers: authHeaders(sessionToken, creatorToken),
    body: JSON.stringify({ title, settings }),
  });
}
