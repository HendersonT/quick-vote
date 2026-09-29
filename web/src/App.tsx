import { useEffect, useState } from "react";
import Home from "./pages/Home";
import ResultsView from "./pages/ResultsView";
import Room from "./pages/Room";

/**
 * Minimal client-side router (no dependency needed for three routes):
 *   "/"                 -> Home
 *   "/v/:slug"          -> Room
 *   "/v/:slug/results"  -> ResultsView (read-only, no join gate)
 * Anything else -> a friendly not-found screen.
 */

type Route =
  | { name: "home" }
  | { name: "room"; slug: string }
  | { name: "results"; slug: string }
  | { name: "not-found" };

export function parseRoute(pathname: string): Route {
  if (pathname === "/") return { name: "home" };
  const match = pathname.match(/^\/v\/([^/]+)\/?$/);
  if (match) return { name: "room", slug: decodeURIComponent(match[1]) };
  const results = pathname.match(/^\/v\/([^/]+)\/results\/?$/);
  if (results) return { name: "results", slug: decodeURIComponent(results[1]) };
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
      // Keyed by slug so moving between votes (e.g. the next-vote handoff)
      // mounts a fresh room instead of carrying state across.
      return <Room key={route.slug} slug={route.slug} />;
    case "results":
      return <ResultsView key={route.slug} slug={route.slug} />;
    default:
      return <NotFound />;
  }
}
