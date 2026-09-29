import { ApiError } from "../api";

interface LoadFailedProps {
  /** Whatever the initial snapshot fetch rejected with. */
  error: unknown;
  /** What the page was loading, for the generic heading ("Couldn't load …"). */
  what: string;
}

/**
 * Full-page message for a room or results page whose first fetch failed.
 * Only a 404 means the vote doesn't exist; anything else (a 500, a network
 * error) says nothing about that, so it gets a generic "couldn't load"
 * instead of a misleading "not found".
 */
export default function LoadFailed({ error, what }: LoadFailedProps) {
  const notFound = error instanceof ApiError && error.status === 404;
  return (
    <main className="not-found">
      <h1>{notFound ? "Vote not found" : `Couldn't load ${what}`}</h1>
      <p>
        {notFound
          ? "Check the link — the vote may have been deleted."
          : "Something went wrong. Try reloading the page."}
      </p>
      <a href="/">Back home</a>
    </main>
  );
}
