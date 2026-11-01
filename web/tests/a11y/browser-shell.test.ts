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

const CHROME_CANDIDATES = ["google-chrome-stable", "google-chrome", "chromium", "chromium-browser"] as const;

/** The measured states: the account screen in both themes at the phone width
 * (the recipe's two load-bearing declarations live there), the front door at
 * the phone width (the one disclosure whose tail is the recipe's own), and
 * the bar at desktop width. */
const states = [
  { hash: "#account", width: 375, theme: "light" },
  { hash: "#account", width: 375, theme: "dark" },
  { hash: "", width: 375, theme: "light" },
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
  avatars: { width: number; height: number; radius: string; src: string }[];
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

  const avatars = await evaluate<FrontDoor["avatars"]>(`[...document.querySelectorAll(".sample-row button .sample-avatar")].map((img) => {
    const box = img.getBoundingClientRect();
    const cs = getComputedStyle(img);
    return {
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

      // The sample people: a circular avatar chip on every button. The bytes
      // are never asserted — the rig's server answers /media with the app
      // shell, so only the box, the circle and the served path are honest.
      expect(door.avatars, "every sample button carries an avatar").toHaveLength(5);
      for (const avatar of door.avatars) {
        expect(avatar.radius, "an avatar is drawn as a circle").toBe("999px");
        expect(avatar.width, "an avatar is a 2.25rem chip").toBeGreaterThan(30);
        expect(avatar.width, "an avatar is a 2.25rem chip").toBeLessThan(42);
        expect(Math.abs(avatar.width - avatar.height), "an avatar is round, not oval").toBeLessThanOrEqual(1);
        expect(avatar.src, "an avatar names a served media path").toMatch(/^\/media\/[0-9a-f]+$/);
      }
    },
  );
}
