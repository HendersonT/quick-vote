import { useEffect, useState } from "react";
import Home from "./pages/Home";

/**
 * Minimal client-side router (no dependency needed for two routes):
 *   "/"          -> Home
 *   "/v/:slug"   -> Room (wired in a later task; placeholder for now)
 * Anything else -> a friendly not-found screen.
 */

type Route =
  | { name: "home" }
  | { name: "room"; slug: string }
  | { name: "not-found" };

function parseRoute(pathname: string): Route {
  if (pathname === "/") return { name: "home" };
  const match = pathname.match(/^\/v\/([^/]+)\/?$/);
  if (match) return { name: "room", slug: decodeURIComponent(match[1]) };
  return { name: "not-found" };
}

/** Pushes a new path onto history and notifies the router to re-render. */
export function navigate(path: string): void {
  window.history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

function usePathname(): string {
  const [pathname, setPathname] = useState(window.location.pathname);

  useEffect(() => {
    const onPopState = () => setPathname(window.location.pathname);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  return pathname;
}

function RoomPlaceholder({ slug }: { slug: string }) {
  // Task 11 replaces this with the real Room page (join gate, phases, etc.).
  return <div className="room-placeholder">Loading vote {slug}…</div>;
}

function NotFound() {
  return (
    <div className="not-found">
      <h1>Page not found</h1>
      <a href="/" onClick={(e) => { e.preventDefault(); navigate("/"); }}>
        Back home
      </a>
    </div>
  );
}

export default function App() {
  const pathname = usePathname();
  const route = parseRoute(pathname);

  switch (route.name) {
    case "home":
      return <Home />;
    case "room":
      return <RoomPlaceholder slug={route.slug} />;
    default:
      return <NotFound />;
  }
}
