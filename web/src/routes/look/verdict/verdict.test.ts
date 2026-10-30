import { isTerminalStatus, type JobEvent, type JobStream } from "@nrynss/chaaya/keel";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { humanMessage } from "../../../lib/api/errors";
import { installMock, pinAnswer } from "../../../lib/api/mock";
import type { ApiFixture } from "../../../lib/api/types";
import { openingSentence, pausedSentence, readingSentence, stageOrder, stageWord, verdictLines } from "./copy";
import { followLook, loadVerdict, storyJobId, storyLookId, type LookFollow } from "./follow";
import { verdictHash } from "./route";

const here = path.dirname(fileURLToPath(import.meta.url));

let page: { setVisibility: (state: "hidden" | "visible") => void; remove: () => void } | undefined;
let follow: LookFollow | undefined;

afterEach(() => {
  follow?.close();
  follow = undefined;
  page?.remove();
  page = undefined;
  installMock()();
});

function installPage(): void {
  let visibility: "hidden" | "visible" = "visible";
  const documentTarget = new EventTarget();
  const windowTarget = new EventTarget();
  const documentShim = {
    get visibilityState(): "hidden" | "visible" {
      return visibility;
    },
    get hidden(): boolean {
      return visibility === "hidden";
    },
    addEventListener: documentTarget.addEventListener.bind(documentTarget),
    removeEventListener: documentTarget.removeEventListener.bind(documentTarget),
    dispatchEvent: documentTarget.dispatchEvent.bind(documentTarget),
  };
  const windowShim = {
    navigator: { onLine: true },
    addEventListener: windowTarget.addEventListener.bind(windowTarget),
    removeEventListener: windowTarget.removeEventListener.bind(windowTarget),
    dispatchEvent: windowTarget.dispatchEvent.bind(windowTarget),
  };
  Object.defineProperty(globalThis, "document", { configurable: true, value: documentShim });
  Object.defineProperty(globalThis, "window", { configurable: true, value: windowShim });
  page = {
    setVisibility(state) {
      visibility = state;
      documentShim.dispatchEvent(new Event("visibilitychange"));
    },
    remove() {
      Reflect.deleteProperty(globalThis, "document");
      Reflect.deleteProperty(globalThis, "window");
    },
  };
}

function progressStages(stream: JobStream): string[] {
  return stream.events.filter((event): event is JobEvent & { name: "progress" } => event.name === "progress").map((event) => event.stage);
}

function sameJob(stream: JobStream, jobId: string): boolean {
  return stream.events.every((event) => event.name === "heartbeat" || event.jobId === jobId);
}

function shownWords(stream: JobStream): string[] {
  return progressStages(stream).map((stage) => stageWord(stage)).filter((word) => word !== "");
}

async function waitUntil(label: string, ready: () => boolean): Promise<void> {
  const deadline = Date.now() + 2000;
  while (!ready()) {
    if (Date.now() > deadline) throw new Error(label);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

/** The recorded event body, held open once the render frame has been sent. */
function holdThroughRender(text: string): ReadableStream<Uint8Array> {
  const frames = text.split("\n\n").filter((piece) => piece.trim() !== "");
  let end = frames.findIndex((piece) => piece.includes('"stage":"render"'));
  if (end < 0) end = frames.length - 1;
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const piece of frames.slice(0, end + 1)) controller.enqueue(encoder.encode(`${piece}\n\n`));
    },
  });
}

test("the stage words are the four look words and nothing else", () => {
  expect(stageWord("judge")).toBe("Checking");
  expect(stageWord("pick")).toBe("Choosing a fix");
  expect(stageWord("measure")).toBe("Choosing a fix");
  expect(stageWord("render")).toBe("Rendering");
  expect(stageWord("check")).toBe("Checking the render");
  expect(stageWord("shop")).toBe("");
  expect(stageWord("delete")).toBe("");
  expect(stageWord(undefined)).toBe("");
  expect([pausedSentence, openingSentence, readingSentence].join(" ")).not.toMatch(/\d/);
  // The stepper walks the same four words, in order.
  expect(stageOrder).toEqual(["Checking", "Choosing a fix", "Rendering", "Checking the render"]);
  expect(stageOrder.join(" ")).not.toMatch(/\d/);
});

test("hiding the page mid-render pauses the same look, and the verdict has no digit", async () => {
  installPage();
  installMock();
  const inner = globalThis.fetch;
  const calls: { path: string; lastEventId: string | null }[] = [];
  let events = 0;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const url = new URL(raw, "http://dresscode.local");
    const response = await inner(input, init);
    if (!url.pathname.endsWith("/events")) return response;
    events += 1;
    calls.push({ path: url.pathname, lastEventId: new Headers(init?.headers).get("last-event-id") });
    if (events > 1) return response;
    const text = await response.text();
    return new Response(holdThroughRender(text), { status: response.status, headers: response.headers });
  }) as typeof fetch;

  const lookAtStart = storyLookId;
  const jobAtStart = storyJobId;
  follow = followLook(lookAtStart, jobAtStart, () => {});
  const stream = follow.stream;

  await waitUntil("render frame", () => {
    const stages = progressStages(stream);
    return stream.connection === "live" && stages.includes("render") && !stages.includes("check");
  });
  expect(shownWords(stream)).toEqual(["Checking", "Choosing a fix", "Choosing a fix", "Rendering"]);
  expect(follow.lookId).toBe(lookAtStart);
  expect(follow.jobId).toBe(jobAtStart);
  expect(sameJob(stream, jobAtStart)).toBe(true);

  page?.setVisibility("hidden");
  expect(stream.connection).toBe("paused");
  expect(follow.stream).toBe(stream);
  expect(follow.lookId).toBe(lookAtStart);
  expect(follow.jobId).toBe(jobAtStart);
  await new Promise((resolve) => setTimeout(resolve, 600));
  expect(stream.connection).toBe("paused");
  expect(calls).toHaveLength(1);
  expect(progressStages(stream)).not.toContain("check");
  expect(stream.status).not.toBe("done");

  page?.setVisibility("visible");
  await waitUntil("resumed verdict", () => stream.status === "done");
  expect(stream.connection).not.toBe("paused");
  expect(follow.stream).toBe(stream);
  expect(follow.lookId).toBe(lookAtStart);
  expect(follow.jobId).toBe(jobAtStart);
  expect(calls.map((call) => call.path)).toEqual([
    `/api/jobs/${jobAtStart}/events`,
    `/api/jobs/${jobAtStart}/events`,
  ]);
  expect(calls[0]?.lastEventId).toBeNull();
  expect(calls[1]?.lastEventId).not.toBeNull();
  expect(sameJob(stream, jobAtStart)).toBe(true);
  expect(shownWords(stream)).toEqual([
    "Checking",
    "Choosing a fix",
    "Choosing a fix",
    "Rendering",
    "Checking the render",
  ]);

  const verdict = await loadVerdict(follow.lookId);
  expect(verdict.lookId).toBe(lookAtStart);
  const text = verdictLines(verdict).join("\n");
  expect(text).not.toMatch(/\d/);
  expect(verdict.reasons).toEqual([
    "The ear reads plain; try gold jhumkas.",
    "The lip reads bare; try a red lip.",
  ]);
  expect(verdict.fix).toBe("Adds gold jhumkas.");
  expect(verdict.why).toBe("The ear reads plain; try gold jhumkas.");
  expect(`${text}\n${pausedSentence}\n${shownWords(stream).join("\n")}`).not.toMatch(/\d/);
});

test("a replacement attempt keeps the look", async () => {
  installPage();
  installMock();
  const first = "a".repeat(32);
  const second = "b".repeat(32);
  const state = (id: string, next?: string): ApiFixture => ({
    file: `${id}-state`,
    method: "GET",
    path: `/api/jobs/${id}`,
    status: 200,
    body: { job_id: id, status: "running", stage: "render", ...(next ? { next_id: next } : {}) },
  });
  const events = (id: string): ApiFixture => ({
    file: `${id}-events`,
    method: "GET",
    path: `/api/jobs/${id}/events`,
    status: 200,
    headers: { "content-type": "text/event-stream" },
    body: `event: progress\nid: 1\ndata: {"job_id":"${id}","stage":"render"}\n\n`,
  });
  pinAnswer("GET", `/api/jobs/${first}`, state(first, second));
  pinAnswer("GET", `/api/jobs/${first}/events`, events(first));
  pinAnswer("GET", `/api/jobs/${second}`, state(second));
  pinAnswer("GET", `/api/jobs/${second}/events`, events(second));

  const seen: string[] = [];
  follow = followLook(storyLookId, first, (_stream, id) => {
    seen.push(id);
  });
  await waitUntil("replacement job", () => follow?.jobId === second);
  expect(follow.lookId).toBe(storyLookId);
  expect(seen[0]).toBe(first);
  expect(follow.jobId).toBe(second);
});

test("a bare ending without an error clears the wait and says so", async () => {
  installPage();
  installMock();

  for (const [job, ending] of [
    ["10b0000000000000000000000000000c", "cancelled"],
    ["10b0000000000000000000000000000d", "interrupted"],
  ] as const) {
    follow = followLook(storyLookId, job, () => {});
    const stream = follow.stream;
    await waitUntil(`${ending} frame`, () => stream.status === ending);
    expect(stream.error).toBeUndefined();
    // The stage word had been on its way, and the job can move no further.
    expect(stageWord(stream.stage)).not.toBe("");
    expect(isTerminalStatus(stream.status)).toBe(true);
    follow?.close();
    follow = undefined;
  }

  // The screen gates the stage word and the opening hint on the same terminal set,
  // and a bare ending shows its own sentence through the usual failure path.
  const screen = readFileSync(path.join(here, "verdict.svelte"), "utf8");
  expect(screen).toContain("!isTerminalStatus(stream.status)");
  expect(screen).toContain('{ code: "job_failed" }');
  const sentence = humanMessage({ code: "job_failed" });
  expect(sentence).not.toBe("");
  const markup = screen.slice(screen.indexOf("</script>")).replace(/<style>[\s\S]*<\/style>/, "");
  const staticText = markup
    .replace(/\{#[^}]*\}/g, " ")
    .replace(/\{:[^}]*\}/g, " ")
    .replace(/\{\/[^}]*\}/g, " ")
    .replace(/\{[^}]*\}/g, " ")
    .replace(/<[^>]+>/g, " ");
  expect(`${sentence}\n${staticText}`).not.toMatch(/\d/);
});

test("the screen and the shell link show no digit", () => {
  const screen = readFileSync(path.join(here, "verdict.svelte"), "utf8");
  const markup = screen.slice(screen.indexOf("</script>")).replace(/<style>[\s\S]*<\/style>/, "");
  const staticText = markup
    .replace(/\{#[^}]*\}/g, " ")
    .replace(/\{:[^}]*\}/g, " ")
    .replace(/\{\/[^}]*\}/g, " ")
    .replace(/\{[^}]*\}/g, " ")
    .replace(/<[^>]+>/g, " ");
  expect(staticText).not.toMatch(/\d/);
  expect(staticText).toContain("The verdict");
  expect(staticText).toContain("What the dress code still asks for");
  expect(staticText).toContain("A fix");
  expect(staticText).toContain("Why");
  expect(screen).toContain("stageWord(");
  expect(screen).toContain("pausedSentence");
  expect(screen).toContain("{verdict.fix}");
  expect(screen).toContain("{verdict.why}");
  expect(screen).toContain("{reason}");
  expect(screen).not.toContain("stream.current");
  expect(screen).not.toContain("stream.total");
  expect(screen).not.toContain("stream.reconnects");
  expect(markup).not.toContain("lookId");
  expect(markup).not.toContain("jobId");

  // The live path: four words, the one in hand named as the step, the hero
  // carrying the fix, and the finished verdict never a dead end.
  expect(screen).toContain('<ol class="stages"');
  expect(screen).toContain("aria-current=");
  expect(screen).toContain('<div class="hero">');
  expect(screen).toContain('{verdict.fix}</p>');
  expect(screen).toContain('<a class="primary-link" href={renderHash}>See the render</a>');
  expect(staticText).toContain("See the render");
  expect(markup).toContain("renderHash");
  const heroStyles = screen.slice(screen.indexOf("<style>"));
  expect(heroStyles).toMatch(/\.hero\s*\{[^}]*background: var\(--raised\)/);
  expect(heroStyles).toMatch(/\.hero\s*\{[^}]*border: 1px solid var\(--line-soft\)/);
  expect(heroStyles).toMatch(/\.hero\s*\{[^}]*border-left: 3px solid var\(--accent\)/);
  expect(heroStyles).toMatch(/\.hero\s*\{[^}]*border-radius: var\(--radius-control\)/);
  expect(heroStyles).toMatch(/\.primary-link\s*\{[^}]*color: var\(--on-accent\)/);
  expect(heroStyles).toMatch(/\.primary-link\s*\{[^}]*background: var\(--accent\)/);
  // The pulse is motion the reader asked for, never one forced on them.
  expect(heroStyles).toMatch(
    /@media \(prefers-reduced-motion: no-preference\)\s*\{[\s\S]*?animation: stage-pulse/,
  );

  const shell = readFileSync(path.join(here, "../../../app/app.svelte"), "utf8");
  expect(shell).toContain("verdictHash");
  expect(shell).toContain("See the verdict");
  expect(shell).toContain('route === "/look/verdict"');
  expect(verdictHash).toBe("#/look/verdict");
});
