// Regenerates the README screenshots in docs/screenshots/ from a throwaway
// local instance seeded with sample data. Never point this at a real
// deployment: it creates votes and writes ballots.
//
// Dev-only (excluded from the Docker build context). Playwright isn't a repo
// dependency, so run it through npx with a version whose bundled Chromium is
// installed (1.62.x uses chromium-1234; otherwise `npx playwright install
// chromium` first):
//
//   npx -y -p playwright@1.62.0 node scripts/screenshots.mjs
//
// Pass --skip-web-build to reuse an existing web/dist build.

import { spawn, execFileSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outDir = join(root, "docs", "screenshots");
const addr = "127.0.0.1:18090";
const base = `http://${addr}`;
const viewport = { width: 1200, height: 800 };

// loadPlaywright finds the `playwright` package either normally or in the
// npx cache: `npx -p` only puts the package's bin dir on PATH, and ESM
// imports ignore NODE_PATH, so resolve from that bin dir explicitly.
function loadPlaywright() {
  const candidates = [join(root, "scripts", "x.js")];
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
    if (dir.endsWith(join("node_modules", ".bin"))) candidates.push(join(dir, "..", "x.js"));
  }
  for (const from of candidates) {
    try {
      return createRequire(from)("playwright");
    } catch {
      // try the next location
    }
  }
  throw new Error("playwright not found; run via: npx -y -p playwright@1.62.0 node scripts/screenshots.mjs");
}

const { chromium } = loadPlaywright();

const tmp = mkdtempSync(join(process.env.TMPDIR ?? tmpdir(), "qv-screens-"));
let server;

async function api(method, path, { body, token, creatorToken } = {}) {
  const headers = { "Content-Type": "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (creatorToken) headers["X-Creator-Token"] = creatorToken;
  const res = await fetch(base + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}

async function waitForServer() {
  for (let i = 0; i < 100; i++) {
    try {
      const res = await fetch(`${base}/`);
      if (res.ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("server did not start");
}

// newPage opens a light-theme browser context, optionally already holding
// a participant session for the given vote (same localStorage shape the SPA
// writes after joining).
async function newPage(browser, slug, session) {
  const ctx = await browser.newContext({ viewport, colorScheme: "light", deviceScaleFactor: 1 });
  if (slug && session) {
    await ctx.addInitScript(
      ([k, v]) => localStorage.setItem(k, v),
      [`qv:${slug}`, JSON.stringify(session)],
    );
  }
  const page = await ctx.newPage();
  page.on("pageerror", (e) => console.error("page error:", e));
  page.on("console", (m) => m.type() === "error" && console.error("console error:", m.text()));
  return page;
}

try {
  // Embedding the real UI means copying it over webembed/dist, whose
  // index.html is a committed placeholder: put the placeholder back once the
  // binary is built so the script leaves the working tree clean.
  const placeholder = join(root, "webembed", "dist", "index.html");
  const placeholderHTML = readFileSync(placeholder);
  const bin = join(tmp, "quickvote");
  try {
    if (!process.argv.includes("--skip-web-build")) {
      execFileSync("npm", ["run", "build"], { cwd: join(root, "web"), stdio: "inherit" });
    }
    cpSync(join(root, "web", "dist"), join(root, "webembed", "dist"), { recursive: true });
    // Build then exec the binary (rather than `go run`) so killing it really
    // stops the server instead of orphaning go run's child.
    execFileSync("go", ["build", "-o", bin, "./cmd/quickvote"], { cwd: root, stdio: "inherit" });
  } finally {
    writeFileSync(placeholder, placeholderHTML);
  }
  server = spawn(bin, ["-addr", addr, "-db", join(tmp, "demo.db")], { stdio: "inherit" });
  await waitForServer();

  // Seed: Alice creates, Bob and Carol join, four games suggested.
  const created = await api("POST", "/api/votes", {
    body: {
      title: "Friday game night",
      creatorName: "Alice",
      // Manual advance keeps the room in voting after all three ballots, so
      // the voting screen can be captured before results.
      settings: { voteAdvanceMode: "manual" },
    },
  });
  const slug = created.slug;
  const alice = created.sessionToken;
  const creatorToken = created.creatorToken;
  const bob = (await api("POST", `/api/votes/${slug}/join`, { body: { name: "Bob" } })).sessionToken;
  const carol = (await api("POST", `/api/votes/${slug}/join`, { body: { name: "Carol" } })).sessionToken;
  for (const [token, title] of [
    [alice, "Catan"],
    [bob, "Wingspan"],
    [carol, "Azul"],
    [carol, "Codenames"],
  ]) {
    await api("POST", `/api/votes/${slug}/suggestions`, { body: { title }, token });
  }
  await api("POST", `/api/votes/${slug}/advance`, { body: {}, token: alice, creatorToken });

  const state = await api("GET", `/api/votes/${slug}`, { token: alice });
  const id = Object.fromEntries(state.options.map((o) => [o.title, o.id]));
  // Budget is 3 credits × 4 options = 12; quadratic cost, so these ballots
  // spend 9, 10 and 9 credits and give Wingspan a clear win.
  const ballots = [
    [alice, { [id.Catan]: 2, [id.Wingspan]: 2, [id.Azul]: 1 }],
    [bob, { [id.Wingspan]: 3, [id.Catan]: 1 }],
    [carol, { [id.Wingspan]: 2, [id.Codenames]: 2, [id.Catan]: 1 }],
  ];
  for (const [token, votes] of ballots) {
    await api("PUT", `/api/votes/${slug}/ballot`, { body: { votes }, token });
  }

  mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch();
  try {
    const home = await newPage(browser);
    await home.goto(`${base}/`);
    await home.fill("#title", "Friday game night");
    await home.fill("#creatorName", "Alice");
    await home.screenshot({ path: join(outDir, "create.png") });

    const bobPage = await newPage(browser, slug, { sessionToken: bob, name: "Bob" });
    await bobPage.goto(`${base}/v/${slug}`);
    await bobPage.waitForSelector(".phase-voting");
    await bobPage.waitForLoadState("networkidle");
    await bobPage.screenshot({ path: join(outDir, "voting.png") });

    await api("POST", `/api/votes/${slug}/advance`, { body: {}, token: alice, creatorToken });
    await bobPage.waitForSelector(".phase-results");
    await bobPage.waitForLoadState("networkidle");
    await bobPage.screenshot({ path: join(outDir, "results.png") });
  } finally {
    await browser.close();
  }
  console.log(`screenshots written to ${outDir}`);
} finally {
  if (server) {
    server.kill("SIGTERM");
    await new Promise((r) => (server.exitCode !== null ? r() : server.once("exit", r)));
  }
  if (existsSync(tmp)) rmSync(tmp, { recursive: true, force: true });
}
