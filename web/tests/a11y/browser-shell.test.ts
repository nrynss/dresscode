// @vitest-environment node
// Node's environment, not jsdom's: this file drives a real Chrome over CDP,
// and it needs Node's own WebSocket and fetch — jsdom's stand-ins refuse the
// DevTools socket. No DOM test lives here; the page is measured in Chrome.
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { createServer, type Server } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterAll, beforeAll, expect, test } from "vitest";

/** jsdom lays nothing out, so this suite is the one place the shell's geometry
 * claims are measured: google-chrome-stable headless, driven over raw CDP on
 * Node's built-in WebSocket — no new dependency (feedback.md's rig). It guards
 * the two recipe declarations whose deletion leaves every string pin green:
 * `align-self: stretch` on `.disclosure > summary` (the account screen's
 * summaries collapse to their text without it) and the `:has(.chevron)`
 * exception (that card grows a second tail without it) — plus the fixed tab
 * bar's hold on the phone column, which jsdom cannot see at all. */

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../..");

/** The chip faces the front door must draw, derived from the sources the
 * browser build itself reads: the recorded sample list names each sample,
 * the app's chip table maps it to a media path, and the sample-media map in
 * `web/vite.config.ts` is the set of paths that answer with bytes. The rig
 * reads the tables rather than restating them, so a mapping edited at
 * either source changes the expectation with it. */
const recordedSampleNames = new Map<string, string>(
  (
    JSON.parse(readFileSync(path.resolve(webRoot, "../testdata/api/samples.json"), "utf8")) as {
      body: { samples: { id: string; name: string }[] };
    }
  ).body.samples.map((sample) => [sample.id, sample.name]),
);
const chipTable = new Map<string, string>();
for (const match of readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8").matchAll(
  /"(sample-[a-z-]+)":\s*"(\/media\/[0-9a-f]+)"/g,
)) {
  chipTable.set(match[1]!, match[2]!);
}
const servedMedia = new Set<string>();
for (const match of readFileSync(path.join(webRoot, "vite.config.ts"), "utf8").matchAll(
  /"(\/media\/[0-9a-f]+)":\s*"[^"]+"/g,
)) {
  servedMedia.add(match[1]!);
}
/** Sample name on a drawn button -> the media path that button's face. */
const faceOfLabel = new Map<string, string>(
  [...recordedSampleNames].flatMap(([id, name]) => {
    const face = chipTable.get(id);
    return face ? [[name, face] as const] : [];
  }),
);

const CHROME_CANDIDATES = ["google-chrome-stable", "google-chrome", "chromium", "chromium-browser"] as const;

/** The measured states: the account screen in both themes at the phone width
 * (the recipe's two load-bearing declarations live there), the front door at
 * the phone width (the one disclosure whose tail is the recipe's own), the
 * capture screen in both themes at the phone width (its product drawer is the
 * capture side of the same accordion recipe), and the bar at desktop width. */
const states = [
  { hash: "#account", width: 375, theme: "light" },
  { hash: "#account", width: 375, theme: "dark" },
  { hash: "", width: 375, theme: "light" },
  { hash: "#/capture", width: 375, theme: "light" },
  { hash: "#/capture", width: 375, theme: "dark" },
  { hash: "#/", width: 768, theme: "light" },
] as const;

interface Disclosure {
  cardWidth: number;
  summaryWidth: number;
  hasChevron: boolean;
  after: string;
}

interface Measurement {
  theme: string | undefined;
  innerWidth: number;
  clientWidth: number;
  scrollWidth: number;
  bar: { left: number; right: number; top: number; height: number };
  shellPaddingBottom: number;
  contentBottom: number;
  tabs: { left: number; right: number }[];
  disclosures: Disclosure[];
}

let workspace: string;
let server: Server;
let baseUrl = "";
let chrome: ReturnType<typeof spawn> | undefined;
let socket: WebSocket;
let messageId = 0;
const pending = new Map<number, { resolve: (value: unknown) => void; reject: (error: Error) => void }>();
const pageEvents = new Set<(params: unknown) => void>();

function findChrome(): string {
  const named = process.env.DRESSCODE_CHROME;
  const candidates = named ? [named, ...CHROME_CANDIDATES] : [...CHROME_CANDIDATES];
  for (const candidate of candidates) {
    const probe = spawnSync(candidate, ["--version"], { stdio: "ignore" });
    if (probe.status === 0) return candidate;
  }
  throw new Error(
    "the browser-backed geometry guard needs google-chrome-stable (or set DRESSCODE_CHROME to a Chromium binary)",
  );
}

function send(method: string, params: Record<string, unknown> = {}): Promise<unknown> {
  messageId += 1;
  const id = messageId;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

function onceEvent(method: string): Promise<void> {
  return new Promise((resolve) => {
    const listener = (params: unknown) => {
      if ((params as { method?: string })?.method !== method) return;
      pageEvents.delete(listener);
      resolve();
    };
    pageEvents.add(listener);
  });
}

async function evaluate<T>(expression: string): Promise<T> {
  const reply = (await send("Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  })) as {
    result?: { value?: T };
    exceptionDetails?: { text?: string; exception?: { description?: string } };
  };
  if (reply.exceptionDetails) {
    throw new Error(`the page threw: ${reply.exceptionDetails.exception?.description ?? reply.exceptionDetails.text}`);
  }
  return reply.result?.value as T;
}

let onOrigin = false;

/** One real document load. The listener goes down before the navigation so
 * the load event cannot slip past. */
async function navigate(url: string): Promise<void> {
  const loaded = onceEvent("Page.loadEventFired");
  await send("Page.navigate", { url });
  await loaded;
  onOrigin = url !== "about:blank";
}

/** A full load of one hash at one width and theme. The theme is written to
 * localStorage on the app's own origin (it is opaque on about:blank) and a
 * fresh document is loaded after it, so the head script applies the theme
 * before the app paints; about:blank in between makes every navigation a
 * real load even when only the hash changed. */
async function measure(hash: string, width: number, theme: string): Promise<Measurement> {
  await send("Emulation.setDeviceMetricsOverride", {
    width,
    height: 667,
    deviceScaleFactor: 1,
    mobile: width < 500,
  });
  if (!onOrigin) await navigate(`${baseUrl}/${hash}`);
  await evaluate(`localStorage.setItem("chaaya-theme", ${JSON.stringify(theme)}); true`);
  await navigate("about:blank");
  await navigate(`${baseUrl}/${hash}`);
  // The load event fires while execution contexts are still swapping and
  // before the app's modules mount, so every step here is its own fresh
  // dispatch: first wait out the shell, each probe a new evaluate that binds
  // to whatever context is current now.
  const deadline = Date.now() + 20_000;
  for (;;) {
    try {
      const ready = await evaluate(`document.querySelector("nav.tab-bar") !== null`);
      if (ready) break;
    } catch {
      // A context mid-swap refuses or misanswers; try again on a fresh one.
    }
    if (Date.now() > deadline) throw new Error("the shell never mounted");
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  // Scrolled to the bottom, because that is where a fixed bar could hide
  // content: with the shell's last measure in place the deepest content ends
  // above the bar's top edge; without it the bar covers the page's last line.
  // The scroll repeats until the document stops growing, because a screen
  // fills its data a tick after the shell mounts.
  const settled = await evaluate(`(async () => {
    let last = -1;
    for (let i = 0; i < 60; i++) {
      window.scrollTo(0, document.documentElement.scrollHeight);
      await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      const height = document.documentElement.scrollHeight;
      if (height === last && (window.scrollY > 0 || height <= window.innerHeight)) return true;
      last = height;
    }
    return false;
  })()`);
  if (!settled) throw new Error("the page never settled to a scroll height");
  return JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const doc = document.documentElement;
    const bar = document.querySelector("nav.tab-bar");
    if (!bar) throw new Error("no tab bar on the page");
    const barRect = bar.getBoundingClientRect();
    const shell = document.querySelector(".shell");
    if (!shell) throw new Error("no shell on the page");
    let contentBottom = 0;
    for (const el of shell.querySelectorAll("header.bar, main, main *")) {
      // What a reader can see: a closed drawer's body keeps laid-out boxes
      // that getBoundingClientRect reports, but nothing painted and nothing
      // that can hide under the bar.
      if (!el.checkVisibility({ checkVisibilityCSS: true, checkContentVisibility: true })) continue;
      const r = el.getBoundingClientRect();
      if (r.height > 0) contentBottom = Math.max(contentBottom, r.bottom);
    }
    return JSON.stringify({
      theme: doc.dataset.theme,
      innerWidth: window.innerWidth,
      clientWidth: doc.clientWidth,
      scrollWidth: doc.scrollWidth,
      bar: {
        left: round(barRect.left),
        right: round(barRect.right),
        top: round(barRect.top),
        height: round(barRect.height),
      },
      shellPaddingBottom: parseFloat(getComputedStyle(shell).paddingBottom),
      contentBottom: round(contentBottom),
      tabs: [...document.querySelectorAll(".tab-bar a.tab")].map((tab) => {
        const r = tab.getBoundingClientRect();
        return { left: round(r.left), right: round(r.right) };
      }),
      disclosures: [...document.querySelectorAll(".disclosure")].map((card) => {
        const summary = card.querySelector(":scope > summary");
        if (!summary) throw new Error("a disclosure with no summary");
        return {
          cardWidth: round(card.getBoundingClientRect().width),
          summaryWidth: round(summary.getBoundingClientRect().width),
          hasChevron: summary.querySelector(".chevron") !== null,
          after: getComputedStyle(summary, "::after").content,
        };
      }),
    });
  })()`),
  );
}

beforeAll(async () => {
  // The built app, in mock mode, in a throwaway directory: the gate needs no
  // server of its own, so this one serves the build over loopback and dies
  // with the suite.
  workspace = mkdtempSync(path.join(tmpdir(), "dresscode-browser-guard-"));
  const dist = path.join(workspace, "dist");
  process.env.VITE_DRESSCODE_API = "mock";
  const { build } = await import("vite");
  await build({ root: webRoot, logLevel: "warn", build: { outDir: dist, emptyOutDir: true } });

  const mime = new Map<string, string>([
    [".html", "text/html"],
    [".js", "text/javascript"],
    [".css", "text/css"],
    [".json", "application/json"],
    [".png", "image/png"],
    [".svg", "image/svg+xml"],
    [".webmanifest", "application/manifest+json"],
  ]);
  server = createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://dresscode.local");
    let file = path.join(dist, decodeURIComponent(url.pathname));
    if (!file.startsWith(dist)) {
      response.writeHead(403).end();
      return;
    }
    if (statSync(file, { throwIfNoEntry: false })?.isDirectory()) file = path.join(file, "index.html");
    if (statSync(file, { throwIfNoEntry: false })?.isFile()) {
      response.writeHead(200, { "Content-Type": mime.get(path.extname(file)) ?? "application/octet-stream" });
      response.end(new Uint8Array(readFileSync(file)));
      return;
    }
    response.writeHead(url.pathname.startsWith("/api/") ? 404 : 200, { "Content-Type": "text/html" });
    response.end(new Uint8Array(readFileSync(path.join(dist, "index.html"))));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as { port: number }).port;
  baseUrl = `http://127.0.0.1:${port}`;

  const binary = findChrome();
  const profile = path.join(workspace, "profile");
  chrome = spawn(
    binary,
    [
      "--headless=new",
      "--remote-debugging-port=0",
      `--user-data-dir=${profile}`,
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-background-networking",
      "--disable-component-update",
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  const listening = await new Promise<string>((resolve, reject) => {
    let tail = "";
    chrome!.stderr!.on("data", (chunk: Buffer) => {
      tail += chunk.toString();
      const match = tail.match(/DevTools listening on (ws:\/\/\S+)/);
      if (match) resolve(match[1]!);
    });
    chrome!.once("exit", (code) => reject(new Error(`chrome exited early (${code}):\n${tail}`)));
  });
  const endpoint = new URL(listening);
  const targets = (await (await fetch(`http://127.0.0.1:${endpoint.port}/json/list`)).json()) as {
    type: string;
    webSocketDebuggerUrl: string;
  }[];
  const page = targets.find((target) => target.type === "page");
  if (!page) throw new Error("chrome opened no page target");

  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise<void>((resolve, reject) => {
    socket.addEventListener("open", () => resolve(), { once: true });
    socket.addEventListener("error", () => reject(new Error("the DevTools socket refused")), { once: true });
  });
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(String(event.data)) as {
      id?: number;
      method?: string;
      params?: Record<string, unknown>;
      error?: { message: string };
      result?: unknown;
    };
    if (message.method !== undefined) {
      for (const listener of pageEvents) listener({ ...message.params, method: message.method });
    }
    if (message.id === undefined) return;
    const waiter = pending.get(message.id);
    if (!waiter) return;
    pending.delete(message.id);
    if (message.error) waiter.reject(new Error(message.error.message));
    else waiter.resolve(message.result);
  });
  await send("Page.enable");
  await send("Runtime.enable");

  // A cold profile's first IndexedDB open can hang: the capture screen's
  // upload store opens one database at mount, and a request issued in that
  // first moment never answers (the database is not even created), while
  // every later open succeeds. One throwaway open here warms the storage
  // service, so no screen's store meets the cold path.
  await send("Emulation.setDeviceMetricsOverride", { width: 375, height: 667, deviceScaleFactor: 1, mobile: true });
  await navigate(`${baseUrl}/`);
  await evaluate(`new Promise((resolve) => {
    const open = indexedDB.open("dresscode-warmup", 1);
    open.onsuccess = () => {
      open.result.close();
      indexedDB.deleteDatabase("dresscode-warmup");
      resolve("warm");
    };
    open.onerror = () => resolve("cold:" + String(open.error));
  })`);
}, 240_000);

afterAll(async () => {
  chrome?.kill();
  socket?.close();
  await new Promise<void>((resolve) => server.close(() => resolve()));
  rmSync(workspace, { recursive: true, force: true });
});

function expectBarInside(measured: Measurement, width: number): void {
  expect(measured.innerWidth, "the viewport is the width under test").toBe(width);
  expect(measured.clientWidth, "no horizontal overflow: the document fits").toBe(width);
  expect(measured.scrollWidth, "no horizontal overflow: nothing widens the page").toBeLessThanOrEqual(width);
  expect(measured.bar.left, "the bar starts at the left edge").toBe(0);
  expect(measured.bar.right, "the bar ends at the right edge").toBe(width);
  expect(measured.bar.height, "the bar is really rendered").toBeGreaterThan(40);
  expect(measured.tabs, "the bar shows the four tabs").toHaveLength(4);
  for (const tab of measured.tabs) {
    expect(tab.left, "a tab starts inside the viewport").toBeGreaterThanOrEqual(0);
    expect(tab.right, "a tab ends inside the viewport").toBeLessThanOrEqual(width);
  }
  // The shell's last measure clears the fixed bar, and no rendered content
  // reaches past the bar's top edge.
  expect(measured.shellPaddingBottom, "the shell clears the bar").toBeGreaterThan(measured.bar.height);
  expect(measured.contentBottom, "no content sits under the bar").toBeLessThanOrEqual(measured.bar.top + 1);
}

for (const state of states) {
  const name = state.hash === "" ? "the front door" : state.hash;
  test(
    `in a real browser, ${name} at ${state.width} px holds the shell's geometry, ${state.theme}`,
    { timeout: 60_000 },
    async () => {
      const measured = await measure(state.hash, state.width, state.theme);
      expect(measured.theme, "the theme is forced").toBe(state.theme);
      expectBarInside(measured, state.width);

      // The accordion recipe: every summary spans its card. The account
      // screen is the one that needs `align-self: stretch` — its scoped
      // summary is inline-flex inside a flex-start column, and without the
      // declaration these summaries collapse to their own text (196 px in a
      // 343 px card) while every jsdom test stays green.
      expect(measured.disclosures.length, "the screen shows its disclosure cards").toBeGreaterThan(0);
      for (const card of measured.disclosures) {
        expect(card.cardWidth, "a card is a card, not a shrunk sliver").toBeGreaterThanOrEqual(300);
        expect(card.summaryWidth, "the summary spans the card").toBeGreaterThanOrEqual(card.cardWidth - 4);
        // The tail: a summary with its own `.chevron` must suppress the
        // recipe's `▾`, or the account screen shows two tails on one card.
        if (card.hasChevron) expect(card.after, "an authored tail silences the recipe's").toBe("none");
        else expect(card.after, "the recipe's tail renders where no screen authors one").toContain("▾");
      }
    },
  );
}

/** The front door's drawn geometry, measured the way a reader sees it: the
 * sample drawer open, an occasion picked, the fix menu and the continue link
 * on the page. jsdom lays nothing out, so the full-width CTA, the circular
 * avatar chips and the selected tile's full border are measured here and
 * nowhere else. No assertion reads pixels: an `<img>` load bypasses the mock
 * (feedback.md), so the avatars are judged by box, radius and src alone. */
interface FrontDoor {
  continueWidth: number;
  columnWidth: number;
  tile: {
    top: number;
    right: number;
    bottom: number;
    left: number;
    style: string;
    colour: string;
    background: string;
  };
  accent: string;
  accentSoft: string;
  avatars: { label: string; width: number; height: number; radius: string; src: string }[];
}

async function waitInDocument(predicate: string, label: string): Promise<void> {
  const deadline = Date.now() + 10_000;
  for (;;) {
    try {
      if (await evaluate(predicate)) return;
    } catch {
      // A context mid-swap; try again on a fresh one.
    }
    if (Date.now() > deadline) throw new Error(`${label} never appeared`);
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
}

async function measureFrontDoor(theme: string): Promise<FrontDoor> {
  await send("Emulation.setDeviceMetricsOverride", {
    width: 375,
    height: 667,
    deviceScaleFactor: 1,
    mobile: true,
  });
  // The picker eases its border and fill over 150 ms under
  // `prefers-reduced-motion: no-preference`; reading the picked tile
  // mid-glide would measure a colour between the line and the accent. The
  // reduced world is emulated, so the drawn state is the settled one — and
  // the reduced-motion clause is the one these assertions then measure.
  await send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-reduced-motion", value: "reduce" }],
  });
  if (!onOrigin) await navigate(`${baseUrl}/`);
  await evaluate(`localStorage.setItem("chaaya-theme", ${JSON.stringify(theme)}); true`);
  await navigate("about:blank");
  await navigate(`${baseUrl}/`);
  await waitInDocument(`document.querySelector("nav.tab-bar") !== null`, "the shell");

  // Open the sample drawer, so the chips are laid out as a reader sees them.
  await evaluate(`(() => {
    const summary = [...document.querySelectorAll("details.samples > summary")].find(
      (el) => el.textContent?.trim() === "Sample looks",
    );
    if (!summary) throw new Error("no sample drawer on the front door");
    summary.click();
    return true;
  })()`);
  await waitInDocument(
    `document.querySelectorAll(".sample-row button .sample-avatar").length === 5`,
    "the five sample avatars",
  );

  // Pick the recorded occasion, so the selected tile and the continue link
  // are the live ones.
  await evaluate(`(() => {
    const label = [...document.querySelectorAll("fieldset.cards label")].find(
      (el) => el.textContent?.trim() === "Family function",
    );
    if (!label) throw new Error("no family function card");
    const input = label.querySelector("input");
    if (!input) throw new Error("no card input");
    input.click();
    return true;
  })()`);
  await waitInDocument(`document.querySelector("a.bring") !== null`, "the continue link");

  const avatars = await evaluate<FrontDoor["avatars"]>(`[...document.querySelectorAll(".sample-row button")].map((button) => {
    const img = button.querySelector(".sample-avatar");
    if (!img) throw new Error("a sample button with no avatar");
    const box = img.getBoundingClientRect();
    const cs = getComputedStyle(img);
    return {
      label: (button.textContent ?? "").trim(),
      width: Math.round(box.width * 10) / 10,
      height: Math.round(box.height * 10) / 10,
      radius: cs.borderRadius,
      src: img.getAttribute("src") ?? "",
    };
  })`);
  const door = JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const link = document.querySelector("a.bring");
    if (!link) throw new Error("no continue link");
    const section = link.closest("section");
    if (!section) throw new Error("no section around the continue link");
    const sectionCs = getComputedStyle(section);
    const sectionBox = section.getBoundingClientRect();
    const column = sectionBox.width - parseFloat(sectionCs.paddingLeft) - parseFloat(sectionCs.paddingRight);
    const tile = [...document.querySelectorAll("fieldset.cards label")].find((el) => el.matches(":has(input:checked)"));
    if (!tile) throw new Error("no selected tile");
    const cs = getComputedStyle(tile);
    const probe = document.createElement("span");
    probe.style.display = "none";
    document.body.append(probe);
    const colourOf = (role) => {
      probe.style.color = "var(" + role + ")";
      return getComputedStyle(probe).color;
    };
    const accent = colourOf("--accent");
    const accentSoft = colourOf("--accent-soft");
    probe.remove();
    return JSON.stringify({
      continueWidth: round(link.getBoundingClientRect().width),
      columnWidth: round(column),
      tile: {
        top: round(parseFloat(cs.borderTopWidth)),
        right: round(parseFloat(cs.borderRightWidth)),
        bottom: round(parseFloat(cs.borderBottomWidth)),
        left: round(parseFloat(cs.borderLeftWidth)),
        style: cs.borderTopStyle,
        colour: cs.borderTopColor,
        background: cs.backgroundColor,
      },
      accent,
      accentSoft,
    });
  })()`),
  ) as Omit<FrontDoor, "avatars">;
  return { ...door, avatars };
}

for (const theme of ["light", "dark"] as const) {
  test(
    `in a real browser, the front door's continue link, selected tile and sample avatars hold their drawn geometry, ${theme}`,
    { timeout: 120_000 },
    async () => {
      const door = await measureFrontDoor(theme);

      // The continue CTA spans the phone column with its label centred.
      expect(door.columnWidth, "the picker column is a real phone column").toBeGreaterThan(300);
      expect(door.continueWidth, "the continue link spans the column").toBeGreaterThanOrEqual(door.columnWidth - 1);

      // The picked tile: a clean border all the way round, in the accent,
      // on the soft accent fill — not the crescent sliver on one side.
      for (const side of ["top", "right", "bottom", "left"] as const) {
        expect(door.tile[side], `the selected tile's ${side} border is the full 2 px`).toBeGreaterThan(1.5);
        expect(door.tile[side], `the selected tile's ${side} border is the full 2 px`).toBeLessThan(2.5);
      }
      expect(door.tile.style, "the selected tile's border is solid").toBe("solid");
      expect(door.tile.colour, "the selected tile's border is the accent").toBe(door.accent);
      expect(door.tile.background, "the selected tile sits on the soft accent").toBe(door.accentSoft);

      // The sample people: a circular avatar chip on every button, each chip
      // naming its own recorded person's face. The bytes are never asserted —
      // the rig's server answers /media with the app shell, so only the box,
      // the circle and the served path are honest; that the path's bytes are
      // the right photograph is the sample-faces guard's ground truth, and
      // this is where a chip caught wearing another sample's face turns red.
      expect(door.avatars, "every sample button carries an avatar").toHaveLength(5);
      const drawnFaces = new Set<string>();
      for (const avatar of door.avatars) {
        expect(avatar.radius, "an avatar is drawn as a circle").toBe("999px");
        expect(avatar.width, "an avatar is a 2.25rem chip").toBeGreaterThan(30);
        expect(avatar.width, "an avatar is a 2.25rem chip").toBeLessThan(42);
        expect(Math.abs(avatar.width - avatar.height), "an avatar is round, not oval").toBeLessThanOrEqual(1);
        expect(avatar.src, "an avatar names a served media path").toMatch(/^\/media\/[0-9a-f]+$/);
        expect(servedMedia.has(avatar.src), `${avatar.src} is one the sample-media map serves`).toBe(true);
        const expected = faceOfLabel.get(avatar.label);
        expect(expected, `${avatar.label} is one of the recorded samples`).toBeDefined();
        expect(avatar.src, `${avatar.label}'s chip shows its own recorded person's face`).toBe(expected);
        drawnFaces.add(avatar.src);
      }
      expect(drawnFaces.size, "the five chips show five distinct faces").toBe(5);
    },
  );
}

/** The capture screen's drawn geometry, measured the way a reader sees it.
 * The empty arch with its inner rim and mirror emblem; the two picker pills,
 * each one line tall at 375 px with its icon ahead of the text; then a real
 * photo drawn on a canvas and handed to the gallery input the way a picker
 * would, and the mirror wearing it, the Check look CTA spanning the column,
 * and the product drawer in the accordion card style. jsdom lays nothing out,
 * so these are measured here and nowhere else. No assertion reads image
 * bytes — the preview is judged by presence, box and containment alone. */
interface CaptureIdle {
  frame: { width: number; height: number; radius: string; overflow: string };
  rim: { width: number; style: string; colour: string };
  emblem: { present: boolean; stroke: string };
  pills: { text: string; width: number; height: number; top: number; icon: boolean }[];
  dim: string;
  lineSoft: string;
}

interface CaptureChosen {
  idleGone: boolean;
  preview: {
    left: number;
    top: number;
    right: number;
    bottom: number;
    position: string;
    objectFit: string;
  };
  frame: { left: number; top: number; right: number; bottom: number; width: number };
  check: { width: number; column: number };
  drawer: { isDisclosure: boolean; summary: string; summaryWidth: number; cardWidth: number; tail: string };
}

async function measureCaptureIdle(theme: string): Promise<CaptureIdle> {
  await send("Emulation.setDeviceMetricsOverride", {
    width: 375,
    height: 667,
    deviceScaleFactor: 1,
    mobile: true,
  });
  // Colour and border assertions read the settled drawn state, so the
  // reduced world is emulated before anything is measured (feedback.md).
  await send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-reduced-motion", value: "reduce" }],
  });
  if (!onOrigin) await navigate(`${baseUrl}/#/capture`);
  await evaluate(`localStorage.setItem("chaaya-theme", ${JSON.stringify(theme)}); true`);
  await navigate("about:blank");
  await navigate(`${baseUrl}/#/capture`);
  await waitInDocument(`document.querySelector("nav.tab-bar") !== null`, "the shell");
  await waitInDocument(
    `[...document.querySelectorAll("button")].some((el) => el.textContent?.trim() === "Open camera")`,
    "the idle camera CTA",
  );
  return JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const frame = document.querySelector(".mirror-frame");
    if (!frame) throw new Error("no mirror frame");
    const frameBox = frame.getBoundingClientRect();
    const frameCs = getComputedStyle(frame);
    const rimCs = getComputedStyle(frame, "::after");
    const emblem = document.querySelector(".mirror-idle svg.mirror-mark");
    const emblemStroke = emblem ? getComputedStyle(emblem.querySelector("path")).stroke : "";
    const probe = document.createElement("span");
    probe.style.display = "none";
    document.body.append(probe);
    const colourOf = (role) => {
      probe.style.color = "var(" + role + ")";
      return getComputedStyle(probe).color;
    };
    const dim = colourOf("--dim");
    const lineSoft = colourOf("--line-soft");
    probe.remove();
    return JSON.stringify({
      frame: {
        width: round(frameBox.width),
        height: round(frameBox.height),
        radius: frameCs.borderTopLeftRadius,
        overflow: frameCs.overflow,
      },
      rim: { width: parseFloat(rimCs.borderTopWidth), style: rimCs.borderTopStyle, colour: rimCs.borderTopColor },
      emblem: { present: emblem !== null, stroke: emblemStroke },
      pills: [...document.querySelectorAll("section[aria-label='Your selfie'] label.file")].map((pill) => {
        const box = pill.getBoundingClientRect();
        return {
          text: pill.textContent?.trim() ?? "",
          width: round(box.width),
          height: round(box.height),
          top: round(box.top),
          icon: pill.querySelector("svg") !== null,
        };
      }),
      dim,
      lineSoft,
    });
  })()`),
  );
}

/** Hand one canvas-drawn JPEG to the gallery picker the way a photo picker
 * would: the file lands in the input's own FileList and the change event
 * fires — the same two steps a real selection is. The recorded fixtures
 * answer the flow that follows, so the screen runs its real path: prepare,
 * preview, upload, register. */
async function chooseSelfieFile(): Promise<void> {
  await evaluate(`(async () => {
    const canvas = document.createElement("canvas");
    canvas.width = 640;
    canvas.height = 480;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("no 2d context");
    ctx.fillStyle = "#804030";
    ctx.fillRect(0, 0, 640, 480);
    ctx.fillStyle = "#e8d8b8";
    ctx.beginPath();
    ctx.arc(320, 200, 100, 0, Math.PI * 2);
    ctx.fill();
    const blob = await new Promise((resolve, reject) =>
      canvas.toBlob((made) => (made ? resolve(made) : reject(new Error("no blob"))), "image/jpeg", 0.9),
    );
    const file = new File([blob], "mirror-guard.jpg", { type: "image/jpeg" });
    const input = [...document.querySelectorAll("section[aria-label='Your selfie'] label.file input[type=file]")].find(
      (el) => !el.hasAttribute("capture"),
    );
    if (!input) throw new Error("no gallery input");
    const transfer = new DataTransfer();
    transfer.items.add(file);
    input.files = transfer.files;
    input.dispatchEvent(new Event("change", { bubbles: true }));
    // The one recorded upload carried a full 65536-byte capture; the guard's
    // canvas photo is smaller, so the completion's recorded receipt is
    // rewritten to name the bytes this flow actually sent, and the
    // uploader's own durability check agrees with what it stored.
    let sent = 0;
    const innerFetch = window.fetch;
    window.fetch = async (input, init) => {
      const url = String(input?.url ?? input);
      if ((init?.method ?? "GET") === "PUT" && init?.body && typeof init.body.byteLength === "number") {
        sent = init.body.byteLength;
      }
      const response = await innerFetch(input, init);
      if (url.endsWith("/complete")) {
        const receipt = JSON.parse(await response.text());
        receipt.size_bytes = sent;
        return new Response(JSON.stringify(receipt), { status: response.status, headers: response.headers });
      }
      return response;
    };
    return true;
  })()`);
  await waitInDocument(`document.querySelector("img.mirror-preview") !== null`, "the selfie preview");
  await waitInDocument(
    `[...document.querySelectorAll("button")].some((el) => el.textContent?.trim() === "Check look")`,
    "the check CTA",
  );
}

async function measureCaptureChosen(): Promise<CaptureChosen> {
  return JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const preview = document.querySelector("img.mirror-preview");
    if (!preview) throw new Error("no selfie preview");
    const frame = document.querySelector(".mirror-frame");
    if (!frame) throw new Error("no mirror frame");
    const previewBox = preview.getBoundingClientRect();
    const frameBox = frame.getBoundingClientRect();
    const cs = getComputedStyle(preview);
    const check = [...document.querySelectorAll("button")].find((el) => el.textContent?.trim() === "Check look");
    if (!check) throw new Error("no check button");
    const section = check.closest("section");
    if (!section) throw new Error("no section around the check button");
    const sectionCs = getComputedStyle(section);
    const sectionBox = section.getBoundingClientRect();
    const column = sectionBox.width - parseFloat(sectionCs.paddingLeft) - parseFloat(sectionCs.paddingRight);
    const drawer = document.querySelector("details.product-drawer");
    if (!drawer) throw new Error("no product drawer");
    const summary = drawer.querySelector(":scope > summary");
    if (!summary) throw new Error("no drawer summary");
    return JSON.stringify({
      idleGone: document.querySelector(".mirror-idle") === null,
      preview: {
        left: round(previewBox.left),
        top: round(previewBox.top),
        right: round(previewBox.right),
        bottom: round(previewBox.bottom),
        position: cs.position,
        objectFit: cs.objectFit,
      },
      frame: {
        left: round(frameBox.left),
        top: round(frameBox.top),
        right: round(frameBox.right),
        bottom: round(frameBox.bottom),
        width: round(frameBox.width),
      },
      check: { width: round(check.getBoundingClientRect().width), column: round(column) },
      drawer: {
        isDisclosure: drawer.classList.contains("disclosure"),
        summary: summary.textContent?.trim() ?? "",
        summaryWidth: round(summary.getBoundingClientRect().width),
        cardWidth: round(drawer.getBoundingClientRect().width),
        tail: getComputedStyle(summary, "::after").content,
      },
    });
  })()`),
  );
}

for (const theme of ["light", "dark"] as const) {
  test(
    `in a real browser, the capture screen wears the selfie in the arch, one-line pills and the check CTA, ${theme}`,
    { timeout: 120_000 },
    async () => {
      const idle = await measureCaptureIdle(theme);

      // The empty arch: rounded to the mirror silhouette, clipped, with the
      // inner rim riding the curve in the soft line and the emblem stroked
      // in the quiet role above the camera CTA.
      expect(idle.frame.radius, "the mirror is arched").toBe("999px");
      expect(idle.frame.overflow, "the arch clips what it holds").toBe("hidden");
      expect(idle.frame.width, "the mirror is a phone-width portrait box").toBeGreaterThan(300);
      expect(idle.frame.height, "the mirror is a portrait box").toBeGreaterThan(idle.frame.width);
      expect(idle.rim.width, "the inner rim is a hairline").toBeGreaterThan(0.5);
      expect(idle.rim.width, "the inner rim is a hairline").toBeLessThan(1.5);
      expect(idle.rim.style, "the inner rim is a drawn line").toBe("solid");
      expect(idle.rim.colour, "the inner rim is the soft line").toBe(idle.lineSoft);
      expect(idle.emblem.present, "the idle emblem is drawn").toBe(true);
      expect(idle.emblem.stroke, "the emblem is stroked in the quiet role").toBe(idle.dim);

      // The two picker pills: named in three words or fewer, each carrying
      // its icon, both on one row, and neither wrapping past one line — the
      // resting height is the pill's min-height, and a wrapped label would
      // stand visibly taller than it.
      expect(idle.pills.map((pill) => pill.text), "the two picker pills").toEqual(["Choose photo", "Phone camera"]);
      expect(Math.abs(idle.pills[0]!.top - idle.pills[1]!.top), "the pills share one row").toBeLessThanOrEqual(1);
      for (const pill of idle.pills) {
        expect(pill.icon, `${pill.text} carries its icon`).toBe(true);
        expect(pill.width, `${pill.text} sits inside the phone`).toBeLessThanOrEqual(375);
        expect(pill.height, `${pill.text} holds one line at 375 px`).toBeLessThanOrEqual(48);
      }

      // A chosen photo: the gallery pill's real flow — prepare, preview,
      // upload, register — puts the picture in the arch and stands the main
      // CTA up under the mirror.
      await chooseSelfieFile();
      const chosen = await measureCaptureChosen();
      expect(chosen.idleGone, "the empty-mirror furniture stands down once the photo is in").toBe(true);
      expect(chosen.preview.position, "the preview fills the frame").toBe("absolute");
      expect(chosen.preview.objectFit, "the preview is covered, not letterboxed").toBe("cover");
      expect(chosen.preview.left, "the preview starts inside the arch").toBeGreaterThanOrEqual(chosen.frame.left - 1);
      expect(chosen.preview.top, "the preview starts inside the arch").toBeGreaterThanOrEqual(chosen.frame.top - 1);
      expect(chosen.preview.right, "the preview ends inside the arch").toBeLessThanOrEqual(chosen.frame.right + 1);
      expect(chosen.preview.bottom, "the preview ends inside the arch").toBeLessThanOrEqual(chosen.frame.bottom + 1);
      expect(chosen.check.column, "the check CTA's column is a phone column").toBeGreaterThan(300);
      expect(chosen.check.width, "the check CTA spans the column").toBeGreaterThanOrEqual(chosen.check.column - 1);

      // The product drawer wears the shared accordion card: the recipe's own
      // tail renders on its summary, and the summary spans the card.
      expect(chosen.drawer.isDisclosure, "the product drawer wears the accordion class").toBe(true);
      expect(chosen.drawer.summary, "the drawer names itself for a reader").toBe("Try your own piece");
      expect(chosen.drawer.cardWidth, "the drawer card spans the column").toBeGreaterThanOrEqual(300);
      expect(chosen.drawer.summaryWidth, "the drawer summary spans the card").toBeGreaterThanOrEqual(
        chosen.drawer.cardWidth - 4,
      );
      expect(chosen.drawer.tail, "the accordion tail renders").toContain("▾");
    },
  );
}

/** The render screen's drawn geometry, measured the way a reader sees it: the
 * arched comparator with its two pills at the bottom corners and its handle
 * at collar level, the whole page never wider than the phone at any position
 * of the swipe — both ends included, where the old centred handle overflowed
 * a 375 px screen by two pixels — and the cohesive result card holding the
 * fix, the shop row and the verdict bar inside one accent-edged box. jsdom
 * lays nothing out, so these are measured here and nowhere else. No
 * assertion reads image bytes. */
interface Pill {
  text: string;
  left: number;
  right: number;
  top: number;
  bottom: number;
  insetBottom: string;
  insetLeft: string;
  insetRight: string;
}

interface CardChild {
  name: string;
  left: number;
  right: number;
  top: number;
  bottom: number;
}

interface ComparatorReading {
  theme: string | undefined;
  clientWidth: number;
  scrollWidth: number;
  frame: { left: number; top: number; right: number; bottom: number; width: number; height: number; radius: string };
  pills: Pill[];
  handle: { left: number; right: number; centerY: number; collarDelta: number };
  card: {
    left: number;
    right: number;
    top: number;
    bottom: number;
    borders: { top: number; right: number; bottom: number; left: number; style: string; colour: string };
    accent: string;
    children: CardChild[];
    column: number;
    shopWidth: number;
    segments: { text: string; width: number }[];
  };
}

async function measureComparator(): Promise<ComparatorReading> {
  return JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const doc = document.documentElement;
    const frame = document.querySelector(".frame");
    if (!frame) throw new Error("no comparator on the render screen");
    const fb = frame.getBoundingClientRect();
    const fcs = getComputedStyle(frame);
    const handle = document.querySelector(".handle");
    if (!handle) throw new Error("no handle on the comparator");
    const hb = handle.getBoundingClientRect();
    const borderTop = parseFloat(fcs.borderTopWidth);
    const borderBottom = parseFloat(fcs.borderBottomWidth);
    const collar = fb.top + borderTop + 0.72 * (fb.height - borderTop - borderBottom);
    const pills = [...document.querySelectorAll(".badge")].map((pill) => {
      const b = pill.getBoundingClientRect();
      const cs = getComputedStyle(pill);
      return {
        text: (pill.textContent ?? "").trim(),
        left: round(b.left),
        right: round(b.right),
        top: round(b.top),
        bottom: round(b.bottom),
        insetBottom: cs.bottom,
        insetLeft: cs.left,
        insetRight: cs.right,
      };
    });
    const card = document.querySelector(".result-card");
    if (!card) throw new Error("no result card on the render screen");
    const cb = card.getBoundingClientRect();
    const ccs = getComputedStyle(card);
    const probe = document.createElement("span");
    probe.style.display = "none";
    document.body.append(probe);
    probe.style.color = "var(--accent)";
    const accent = getComputedStyle(probe).color;
    probe.remove();
    const children = [...card.querySelectorAll(":scope > *")].map((el) => {
      const b = el.getBoundingClientRect();
      const name = el.classList.contains("hero")
        ? "hero"
        : el.classList.contains("verdict")
          ? "verdict"
          : el.tagName.toLowerCase();
      return { name, left: round(b.left), right: round(b.right), top: round(b.top), bottom: round(b.bottom) };
    });
    const shop = card.querySelector(".shop-trigger");
    if (!shop) throw new Error("no shop trigger in the result card");
    const shopBox = shop.getBoundingClientRect();
    const segments = [...card.querySelectorAll(".verdict button")].map((button) => {
      const b = button.getBoundingClientRect();
      return { text: (button.textContent ?? "").trim(), width: round(b.width) };
    });
    const column =
      cb.width -
      parseFloat(ccs.paddingLeft) -
      parseFloat(ccs.paddingRight) -
      parseFloat(ccs.borderLeftWidth) -
      parseFloat(ccs.borderRightWidth);
    return JSON.stringify({
      theme: doc.dataset.theme,
      clientWidth: doc.clientWidth,
      scrollWidth: doc.scrollWidth,
      frame: {
        left: round(fb.left),
        top: round(fb.top),
        right: round(fb.right),
        bottom: round(fb.bottom),
        width: round(fb.width),
        height: round(fb.height),
        radius: fcs.borderTopLeftRadius,
      },
      pills,
      handle: {
        left: round(hb.left),
        right: round(hb.right),
        centerY: round(hb.top + hb.height / 2),
        collarDelta: round(hb.top + hb.height / 2 - collar),
      },
      card: {
        left: round(cb.left),
        right: round(cb.right),
        top: round(cb.top),
        bottom: round(cb.bottom),
        borders: {
          top: parseFloat(ccs.borderTopWidth),
          right: parseFloat(ccs.borderRightWidth),
          bottom: parseFloat(ccs.borderBottomWidth),
          left: parseFloat(ccs.borderLeftWidth),
          style: ccs.borderTopStyle,
          colour: ccs.borderTopColor,
        },
        accent,
        children,
        column: round(column),
        shopWidth: round(shopBox.width),
        segments,
      },
    });
  })()`),
  ) as ComparatorReading;
}

async function tapPill(label: string): Promise<void> {
  await evaluate(`(() => {
    const pill = [...document.querySelectorAll("button.badge")].find(
      (el) => (el.textContent ?? "").trim() === ${JSON.stringify(label)},
    );
    if (!pill) throw new Error("no ${label} pill on the comparator");
    pill.click();
    return true;
  })()`);
  await waitInDocument(
    `document.querySelector(".divider")?.style.left === ${JSON.stringify(label === "Before" ? "100%" : "0%")}`,
    `the split at the ${label} end`,
  );
}

function expectPillsAndHandle(reading: ComparatorReading, width: number): void {
  expect(reading.clientWidth, "the phone column is the width under test").toBe(width);
  expect(reading.scrollWidth, "no tapped state widens the page").toBeLessThanOrEqual(width);

  // The two pills sit at the bottom corners of the arch, inside its box.
  expect(reading.pills.map((pill) => pill.text), "the comparator's two pills").toEqual(["Before", "After"]);
  for (const pill of reading.pills) {
    expect(pill.left, `${pill.text} starts inside the arch`).toBeGreaterThanOrEqual(reading.frame.left - 1);
    expect(pill.right, `${pill.text} ends inside the arch`).toBeLessThanOrEqual(reading.frame.right + 1);
    expect(pill.top, `${pill.text} sits below the arch's top curve`).toBeGreaterThan(reading.frame.top);
    expect(pill.bottom, `${pill.text} ends inside the arch`).toBeLessThanOrEqual(reading.frame.bottom + 1);
    expect(pill.bottom, `${pill.text} sits at the bottom of the arch`).toBeGreaterThanOrEqual(reading.frame.bottom - 60);
  }
  // The corners are pinned by the computed insets: Before from the left
  // edge, After from the right edge, both off the bottom.
  expect(reading.pills[0]!.insetLeft, "Before is inset at the left corner").toBe("12px");
  expect(reading.pills[1]!.insetRight, "After is inset at the right corner").toBe("12px");
  expect(reading.pills[0]!.insetBottom, "Before sits off the bottom edge").toBe("12px");
  expect(reading.pills[1]!.insetBottom, "After sits off the bottom edge").toBe("12px");

  // The handle rides the split at collarbone level, clear of the face.
  expect(Math.abs(reading.handle.collarDelta), "the handle sits at collar level").toBeLessThanOrEqual(2);
  expect(reading.handle.left, "the handle starts inside the viewport").toBeGreaterThanOrEqual(0);
  expect(reading.handle.left, "the handle starts inside the arch").toBeGreaterThanOrEqual(reading.frame.left - 1);
  expect(reading.handle.right, "the handle ends inside the viewport").toBeLessThanOrEqual(width);
  expect(reading.handle.right, "the handle ends inside the arch").toBeLessThanOrEqual(reading.frame.right + 1);
}

function expectResultCard(reading: ComparatorReading): void {
  const card = reading.card;
  expect(card.right - card.left, "the result card spans the column").toBeGreaterThanOrEqual(300);
  for (const side of ["top", "right", "bottom", "left"] as const) {
    expect(card.borders[side], `the card's ${side} border is the full 2 px`).toBeGreaterThan(1.5);
    expect(card.borders[side], `the card's ${side} border is the full 2 px`).toBeLessThan(2.5);
  }
  expect(card.borders.style, "the card's border is solid").toBe("solid");
  expect(card.borders.colour, "the card is edged in the accent, all the way round").toBe(card.accent);

  // One cohesive card: every drawn child sits inside its box, the shop row
  // spans the card's column, and the three verdict words share it equally.
  expect(card.children.map((child) => child.name), "the card holds the hero, the shop row and the bar").toEqual([
    "hero",
    "button",
    "verdict",
  ]);
  for (const child of card.children) {
    expect(child.left, `${child.name} starts inside the card`).toBeGreaterThanOrEqual(card.left - 1);
    expect(child.right, `${child.name} ends inside the card`).toBeLessThanOrEqual(card.right + 1);
    expect(child.top, `${child.name} starts inside the card`).toBeGreaterThanOrEqual(card.top - 1);
    expect(child.bottom, `${child.name} ends inside the card`).toBeLessThanOrEqual(card.bottom + 1);
  }
  expect(card.shopWidth, "the shop row spans the card's column").toBeGreaterThanOrEqual(card.column - 1);
  expect(card.segments.map((segment) => segment.text), "the verdict bar's three words").toEqual([
    "Keep",
    "Dismiss",
    "Bought it",
  ]);
  const widest = Math.max(...card.segments.map((segment) => segment.width));
  const narrowest = Math.min(...card.segments.map((segment) => segment.width));
  expect(widest - narrowest, "the three segments are equal").toBeLessThanOrEqual(2);
}

for (const theme of ["light", "dark"] as const) {
  test(
    `in a real browser, the render screen holds the bottom pills, the collar handle and the result card, ${theme}`,
    { timeout: 120_000 },
    async () => {
      await send("Emulation.setDeviceMetricsOverride", { width: 375, height: 667, deviceScaleFactor: 1, mobile: true });
      await send("Emulation.setEmulatedMedia", {
        features: [{ name: "prefers-reduced-motion", value: "reduce" }],
      });
      if (!onOrigin) await navigate(`${baseUrl}/#/look/render`);
      await evaluate(`localStorage.setItem("chaaya-theme", ${JSON.stringify(theme)}); true`);
      await navigate("about:blank");
      await navigate(`${baseUrl}/#/look/render`);
      await waitInDocument(`document.querySelector("nav.tab-bar") !== null`, "the shell");
      await waitInDocument(`document.querySelector(".frame .layer") !== null`, "the comparator");
      await waitInDocument(`document.querySelector(".result-card") !== null`, "the result card");

      // The halfway state the screen opens on.
      const mid = await measureComparator();
      expect(mid.theme, "the theme is forced").toBe(theme);
      expect(mid.frame.radius, "the comparator is arched").toBe("999px");
      expect(mid.frame.height, "the comparator is a portrait box").toBeGreaterThan(mid.frame.width);
      expectPillsAndHandle(mid, 375);
      expectResultCard(mid);

      // A tap on either pill rides the split to that end — and neither end
      // may push the round handle past the arch, the old 2 px overflow.
      await tapPill("Before");
      const before = await measureComparator();
      expect(before.scrollWidth, "the Before end does not widen the page").toBeLessThanOrEqual(375);
      expectPillsAndHandle(before, 375);

      await tapPill("After");
      const after = await measureComparator();
      expect(after.scrollWidth, "the After end does not widen the page").toBeLessThanOrEqual(375);
      expectPillsAndHandle(after, 375);
    },
  );
}

/** The verdict screen's mirror, measured the way a reader sees it: the screen
 * opens on the photograph — the arched comparator when the render stands, the
 * selfie in the arched mirror frame when the look flags nothing — above the
 * screen's words, with the page never wider than the phone. jsdom lays
 * nothing out, so this is measured here and nowhere else. No assertion reads
 * image bytes. */
interface MirrorReading {
  theme: string | undefined;
  clientWidth: number;
  scrollWidth: number;
  mirror: {
    selector: string;
    left: number;
    top: number;
    right: number;
    bottom: number;
    width: number;
    height: number;
    radius: string;
    images: number;
  };
  sitsAbove: boolean;
  card: {
    left: number;
    top: number;
    right: number;
    bottom: number;
    borders: { top: number; right: number; bottom: number; left: number; style: string; colour: string };
    accent: string;
    children: { name: string; left: number; right: number; top: number; bottom: number }[];
    column: number;
    linkWidth: number;
  } | null;
}

/** The recorded no-fix body, served from the page itself: the mock is
 * installed underneath, so the wrap rides on top of it and answers only the
 * look read — the same shape `testdata/api/look-nofix.json` records. */
const noFixWrap = `(() => {
  if (window.__nofixWrapped) return true;
  const inner = window.fetch;
  const body = {
    id: "100c0000000000000000000000000001",
    job_id: "10b00000000000000000000000000001",
    occasion: "family-function",
    selfie_id: "5e1f1e00000000000000000000000001",
    selfie_url: "/media/5e1f1e00000000000000000000000001",
    ready: true,
    flags: [],
    fix: null,
    runner_up: "",
    shown_render: null,
    suggestion_only: true,
    products: [],
    verdict: "",
    words: "",
  };
  window.fetch = async (input, init) => {
    const url = String(input && (input.url ?? input));
    const method = (init && init.method ? init.method : input instanceof Request ? input.method : "GET").toUpperCase();
    if (method === "GET" && url.endsWith("/api/looks/100c0000000000000000000000000001")) {
      return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
    }
    return inner(input, init);
  };
  window.__nofixWrapped = true;
  return true;
})()`;

async function measureVerdictMirror(theme: string, noFix: boolean): Promise<MirrorReading> {
  await send("Emulation.setDeviceMetricsOverride", { width: 375, height: 667, deviceScaleFactor: 1, mobile: true });
  await send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-reduced-motion", value: "reduce" }],
  });
  // The app has to be mounted before the wrap goes on — it rides on top of
  // the installed mock — so the tour lands on the front door first, wraps,
  // and walks to the verdict by hash, which mounts the screen fresh.
  if (!onOrigin) await navigate(`${baseUrl}/#/`);
  await evaluate(`localStorage.setItem("chaaya-theme", ${JSON.stringify(theme)}); true`);
  await navigate("about:blank");
  await navigate(`${baseUrl}/#/`);
  await waitInDocument(`document.querySelector("nav.tab-bar") !== null`, "the shell");
  if (noFix) await evaluate(noFixWrap);
  await evaluate(`location.hash = "#/look/verdict"; true`);
  const picture = noFix
    ? `document.querySelector(".mirror-frame img") !== null`
    : `document.querySelector(".frame .layer") !== null`;
  await waitInDocument(picture, noFix ? "the selfie mirror" : "the arched comparator");
  return JSON.parse(
    await evaluate<string>(`(() => {
    const round = (n) => Math.round(n * 10) / 10;
    const doc = document.documentElement;
    const selector = ${noFix} ? ".mirror-frame" : ".frame";
    const mirror = document.querySelector(selector);
    if (!mirror) throw new Error("no mirror on the verdict screen");
    const mb = mirror.getBoundingClientRect();
    const cs = getComputedStyle(mirror);
    const images = mirror.querySelectorAll("img").length;
    const next = [...document.querySelectorAll(".disclosure, .hero, .result-card")].reduce(
      (top, el) => Math.min(top, el.getBoundingClientRect().top),
      Number.POSITIVE_INFINITY,
    );
    // The fix's card, when the look carries a fix: the same Result Card
    // recipe the render screen speaks, with the fix as its hero and the
    // render link as its way forward.
    const card = document.querySelector(".result-card");
    let cardReading = null;
    if (card) {
      const cb = card.getBoundingClientRect();
      const ccs = getComputedStyle(card);
      const probe = document.createElement("span");
      probe.style.display = "none";
      document.body.append(probe);
      probe.style.color = "var(--accent)";
      const accent = getComputedStyle(probe).color;
      probe.remove();
      const link = card.querySelector("a.primary-link");
      if (!link) throw new Error("no render link in the verdict card");
      cardReading = {
        left: round(cb.left),
        top: round(cb.top),
        right: round(cb.right),
        bottom: round(cb.bottom),
        borders: {
          top: parseFloat(ccs.borderTopWidth),
          right: parseFloat(ccs.borderRightWidth),
          bottom: parseFloat(ccs.borderBottomWidth),
          left: parseFloat(ccs.borderLeftWidth),
          style: ccs.borderTopStyle,
          colour: ccs.borderTopColor,
        },
        accent,
        children: [...card.querySelectorAll(":scope > *")].map((el) => {
          const b = el.getBoundingClientRect();
          return {
            name: el.classList.contains("hero") ? "hero" : el.tagName.toLowerCase(),
            left: round(b.left),
            right: round(b.right),
            top: round(b.top),
            bottom: round(b.bottom),
          };
        }),
        column: round(
          cb.width -
            parseFloat(ccs.paddingLeft) -
            parseFloat(ccs.paddingRight) -
            parseFloat(ccs.borderLeftWidth) -
            parseFloat(ccs.borderRightWidth),
        ),
        linkWidth: round(link.getBoundingClientRect().width),
      };
    }
    return JSON.stringify({
      theme: doc.dataset.theme,
      clientWidth: doc.clientWidth,
      scrollWidth: doc.scrollWidth,
      mirror: {
        selector,
        left: round(mb.left),
        top: round(mb.top),
        right: round(mb.right),
        bottom: round(mb.bottom),
        width: round(mb.width),
        height: round(mb.height),
        radius: cs.borderTopLeftRadius,
        images,
      },
      sitsAbove: mb.bottom <= next + 1,
      card: cardReading,
    });
  })()`),
  ) as MirrorReading;
}

function expectMirror(reading: MirrorReading, width: number, theme: string, noFix: boolean): void {
  expect(reading.theme, "the theme is forced").toBe(theme);
  expect(reading.clientWidth, "the phone column is the width under test").toBe(width);
  expect(reading.scrollWidth, "the mirror does not widen the page").toBeLessThanOrEqual(width);
  expect(reading.mirror.selector, "the picture the look carries").toBe(noFix ? ".mirror-frame" : ".frame");
  expect(reading.mirror.radius, "the mirror is arched").toBe("999px");
  expect(reading.mirror.height, "the mirror is a portrait box").toBeGreaterThan(reading.mirror.width);
  expect(reading.mirror.images, "the photograph is in the mirror").toBeGreaterThan(0);
  expect(reading.mirror.left, "the mirror starts inside the viewport").toBeGreaterThanOrEqual(0);
  expect(reading.mirror.right, "the mirror ends inside the viewport").toBeLessThanOrEqual(width);
  expect(reading.sitsAbove, "the mirror leads the screen, above the words").toBe(true);
}

/** The verdict screen's fix card, when the look carries a fix: the same
 * Result Card language the render screen speaks — one box on the shared
 * recipe, edged all the way round in the accent, holding the fix as its hero
 * and the render link as its way forward, both children inside its box, the
 * link spanning the card's column. This screen holds no shop row and no
 * verdict bar to add, so two children is the whole honest card. */
function expectVerdictCard(reading: MirrorReading): void {
  const card = reading.card;
  expect(card, "the verdict screen's fix lives in a Result Card").not.toBeNull();
  if (!card) return;
  expect(card.right - card.left, "the verdict card spans the column").toBeGreaterThanOrEqual(300);
  for (const side of ["top", "right", "bottom", "left"] as const) {
    expect(card.borders[side], `the verdict card's ${side} border is the full 2 px`).toBeGreaterThan(1.5);
    expect(card.borders[side], `the verdict card's ${side} border is the full 2 px`).toBeLessThan(2.5);
  }
  expect(card.borders.style, "the verdict card's border is solid").toBe("solid");
  expect(card.borders.colour, "the verdict card is edged in the accent, all the way round").toBe(card.accent);
  expect(card.children.map((child) => child.name), "the card holds the fix and the render link").toEqual([
    "hero",
    "a",
  ]);
  for (const child of card.children) {
    expect(child.left, `${child.name} starts inside the card`).toBeGreaterThanOrEqual(card.left - 1);
    expect(child.right, `${child.name} ends inside the card`).toBeLessThanOrEqual(card.right + 1);
    expect(child.top, `${child.name} starts inside the card`).toBeGreaterThanOrEqual(card.top - 1);
    expect(child.bottom, `${child.name} ends inside the card`).toBeLessThanOrEqual(card.bottom + 1);
  }
  expect(card.linkWidth, "the render link spans the card's column").toBeGreaterThanOrEqual(card.column - 1);
}

for (const theme of ["light", "dark"] as const) {
  test(
    `in a real browser, the verdict screen opens on the arched mirror photo, ${theme}`,
    { timeout: 120_000 },
    async () => {
      const reading = await measureVerdictMirror(theme, false);
      expectMirror(reading, 375, theme, false);
      expect(reading.mirror.images, "the comparator holds both pictures").toBe(2);
      expectVerdictCard(reading);
    },
  );

  test(
    `in a real browser, the verdict screen shows the selfie alone when the look flags nothing, ${theme}`,
    { timeout: 120_000 },
    async () => {
      const reading = await measureVerdictMirror(theme, true);
      expectMirror(reading, 375, theme, true);
      expect(reading.mirror.images, "the selfie stands without the comparator").toBe(1);
      expect(reading.card, "the clean look keeps its sentence, no fix card").toBeNull();
    },
  );
}
