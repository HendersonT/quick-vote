import type { RoomState } from "./types";

const MAX_BACKOFF_MS = 15000;

/**
 * Connects to the per-vote WebSocket, invoking `onState` with every
 * personalized snapshot the server pushes. Reconnects automatically with
 * exponential backoff (1s, 2s, 4s, ... capped at 15s); the server resends
 * the full snapshot on every (re)connect.
 *
 * The session token is sent as the first message after the socket opens
 * (`{"type":"auth","token":...}`, empty for spectators) rather than in the
 * URL, so it never lands in proxy or tunnel access logs. The server closes
 * connections that don't authenticate within a few seconds.
 *
 * Returns a cleanup function that closes the socket and stops reconnecting.
 */
export function connectRoom(
  slug: string,
  token: string,
  onState: (state: RoomState) => void,
): () => void {
  let socket: WebSocket | null = null;
  let closed = false;
  let backoffMs = 1000;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  const wsProtocol = location.protocol === "https:" ? "wss" : "ws";
  const url = `${wsProtocol}://${location.host}/api/votes/${slug}/ws`;

  function connect() {
    if (closed) return;
    socket = new WebSocket(url);

    socket.onopen = () => {
      socket?.send(JSON.stringify({ type: "auth", token }));
      backoffMs = 1000;
    };

    socket.onmessage = (event) => {
      try {
        const state = JSON.parse(event.data as string) as RoomState;
        onState(state);
      } catch {
        // ignore malformed messages
      }
    };

    socket.onclose = () => {
      if (closed) return;
      reconnectTimer = setTimeout(connect, backoffMs);
      backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS);
    };

    socket.onerror = () => {
      socket?.close();
    };
  }

  connect();

  return () => {
    closed = true;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    socket?.close();
  };
}
