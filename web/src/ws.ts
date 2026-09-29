import type { RoomState } from "./types";

const MAX_BACKOFF_MS = 15000;

/**
 * The first message on a room socket:
 * `{"type":"auth","token":...,"creatorToken":...}`, with creatorToken only
 * when there is one.
 */
export function authMessage(token: string, creatorToken?: string): string {
  return JSON.stringify(
    creatorToken ? { type: "auth", token, creatorToken } : { type: "auth", token },
  );
}

/**
 * Connects to the per-vote WebSocket, invoking `onState` with every
 * personalized snapshot the server pushes. Reconnects automatically with
 * exponential backoff (1s, 2s, 4s, ... capped at 15s); the server resends
 * the full snapshot on every (re)connect.
 *
 * The session token is sent as the first message after the socket opens
 * (see authMessage; empty for spectators) rather than in the URL, so it
 * never lands in proxy or tunnel access logs. The server closes connections
 * that don't authenticate within a few seconds. Pass the creator token too
 * when the session has one: the creator's snapshots carry the follow-up
 * vote's creator token only to a socket that presented it.
 *
 * Returns a cleanup function that closes the socket and stops reconnecting.
 */
export function connectRoom(
  slug: string,
  token: string,
  onState: (state: RoomState) => void,
  creatorToken?: string,
): () => void {
  let socket: WebSocket | null = null;
  let closed = false;
  let backoffMs = 1000;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  const wsProtocol = location.protocol === "https:" ? "wss" : "ws";
  const url = `${wsProtocol}://${location.host}/api/votes/${slug}/ws`;

  function connect() {
    if (closed) return;
    // Handlers use this socket, not the shared variable, which a reconnect
    // may already have pointed at a newer one.
    const ws = new WebSocket(url);
    socket = ws;

    ws.onopen = () => {
      ws.send(authMessage(token, creatorToken));
      backoffMs = 1000;
    };

    ws.onmessage = (event) => {
      try {
        const state = JSON.parse(event.data as string) as RoomState;
        onState(state);
      } catch {
        // ignore malformed messages
      }
    };

    ws.onclose = () => {
      if (closed) return;
      reconnectTimer = setTimeout(connect, backoffMs);
      backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS);
    };

    ws.onerror = () => {
      ws.close();
    };
  }

  connect();

  return () => {
    closed = true;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    socket?.close();
  };
}
