import { isTerminalStatus, type JobEvent, type JobStream } from "@nrynss/chaaya/keel";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { humanMessage } from "../../../lib/api/errors";
import { installMock, pinAnswer, preferFixture } from "../../../lib/api/mock";
import type { ApiFixture } from "../../../lib/api/types";
import {
  cleanSentence,
  openingSentence,
  pausedSentence,
  readingSentence,
  stageOrder,
  stageWord,
  suggestionOnlySentence,
  verdictFromLook,
  verdictLines,
} from "./copy";
import { cleanSentence as renderCleanSentence } from "../render/copy";
import { followLook, loadVerdict, storyJobId, storyLookId, type LookFollow } from "./follow";
import { occasionHash, verdictHash } from "./route";

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
  expect([pausedSentence, openingSentence, readingSentence, cleanSentence].join(" ")).not.toMatch(/\d/);
  // The look that flags nothing is the good outcome, said in one plain
  // sentence: no exclamation, no digit, nothing to fix. The render screen's
  // copy carries the same sentence, word for word.
  expect(cleanSentence).toBe("Nothing needs changing; the outfit already meets the dress code.");
  expect(cleanSentence).toBe(renderCleanSentence);
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

test("the look that flags nothing is read as the good outcome", async () => {
  installMock();
  preferFixture("GET", `/api/looks/${storyLookId}`, "look-nofix.json");
  const verdict = await loadVerdict(storyLookId);
  expect(verdict.lookId).toBe(storyLookId);
  expect(verdict.fix).toBeUndefined();
  expect(verdict.why).toBeUndefined();
  expect(verdict.reasons).toEqual([]);
  expect(verdict.suggestionOnly).toBe(false);
  // The clean look has no lines to fold into the notes: the sentence is the
  // whole verdict on the page.
  expect(verdictLines(verdict)).toEqual([]);

  // The recorded shape is the server's own answer for a look whose judge
  // flagged nothing (the shape T4.6 pins on the interview sample): the fix
  // key present and JSON null, nothing flagged, nothing shown, nothing to buy.
  const fixtureDir = path.resolve(here, "../../../../../testdata/api");
  const recorded = JSON.parse(readFileSync(path.join(fixtureDir, "look-nofix.json"), "utf8")) as {
    body: Record<string, unknown>;
  };
  expect("fix" in recorded.body).toBe(true);
  expect(recorded.body.fix).toBeNull();
  expect(recorded.body.flags).toEqual([]);
  expect(recorded.body.shown_render).toBeNull();
  expect(recorded.body.products).toEqual([]);
  // The wire's suggestion_only is true here too — it means no render stood,
  // on every path that ends without one — so the flag cannot tell the clean
  // look from the suggestion-only one. The shape does: the verdict above was
  // read from this very body and is not suggestion-only.
  expect(recorded.body.suggestion_only).toBe(true);

  // The fix key may also ride absent altogether, and reads as the clean
  // look when nothing is flagged. Flags that ride without a fix are the
  // suggestion-only outcome, and a verdict too: the judge flagged the look,
  // no fix was open to try. A fix that answers as an empty object is still
  // not a name.
  const absent: Record<string, unknown> = { ...recorded.body };
  absent.fix = undefined;
  const absentRead = verdictFromLook(absent);
  expect(absentRead.suggestionOnly).toBe(false);
  const flagged = {
    ...recorded.body,
    flags: [{ id: "family-lip-colour", kind: "grooming", suggestion: "Try a red lip." }],
  };
  const flaggedRead = verdictFromLook(flagged);
  expect(flaggedRead.suggestionOnly).toBe(true);
  expect(flaggedRead.fix).toBeUndefined();
  expect(flaggedRead.why).toBeUndefined();
  expect(flaggedRead.reasons).toEqual(["Try a red lip."]);
  expect(() => verdictFromLook({ ...recorded.body, fix: {} })).toThrow();

  // The clean verdict is its own branch, the quiet side of the no-fix fork
  // the suggestion-only card shares with it: the sentence stands where the
  // fix hero was, and no link points at a render that does not exist.
  const screen = readFileSync(path.join(here, "verdict.svelte"), "utf8");
  expect(screen).toContain("{#if verdict.fix === undefined}");
  const fork = screen.indexOf("{#if verdict.suggestionOnly}");
  expect(fork).toBeGreaterThan(screen.indexOf("{#if verdict.fix === undefined}"));
  const cleanBranch = screen.slice(
    screen.indexOf("{:else}", fork),
    screen.indexOf("{/if}", screen.indexOf("{:else}", fork)),
  );
  expect(cleanBranch).toContain("{cleanSentence}");
  expect(cleanBranch).not.toContain("hero");
  expect(cleanBranch).not.toContain("renderHash");
  expect(cleanBranch).not.toContain("result-card");
  expect(cleanBranch).not.toContain("occasionHash");
});

test("the suggestion-only look names the cause and hands over the path", async () => {
  installMock();
  // The shape the first real user's look ended in: the judge flagged the
  // look, the occasion's fix menu had nothing ticked open, so no fix was
  // picked — the wire carries the flags, a null fix, and suggestion_only
  // true. Before this task the reader refused the shape whole, and the
  // screen answered "The verdict could not be read.": the dead end.
  pinAnswer("GET", `/api/looks/${storyLookId}`, {
    file: "look-suggestion-only",
    method: "GET",
    path: `/api/looks/${storyLookId}`,
    status: 200,
    body: {
      id: storyLookId,
      job_id: storyJobId,
      occasion: "family-function",
      selfie_id: "5e1f1e00000000000000000000000001",
      selfie_url: "/media/5e1f1e00000000000000000000000001",
      ready: true,
      flags: [
        { id: "family-earring-festive", kind: "grooming", suggestion: "The ear reads plain; try gold jhumkas." },
        { id: "family-lip-colour", kind: "grooming", suggestion: "The lip reads bare; try a red lip." },
      ],
      fix: null,
      runner_up: "",
      shown_render: null,
      suggestion_only: true,
      products: [],
      verdict: "",
      words: "",
    },
  });
  const verdict = await loadVerdict(storyLookId);
  expect(verdict.suggestionOnly).toBe(true);
  expect(verdict.fix).toBeUndefined();
  expect(verdict.why).toBeUndefined();
  expect(verdict.reasons).toEqual([
    "The ear reads plain; try gold jhumkas.",
    "The lip reads bare; try a red lip.",
  ]);
  // The suggestions still show, as reasons worded as suggestions, and the
  // sentence naming the cause rides with them. No digit anywhere.
  const text = verdictLines(verdict).join("\n");
  expect(text).toContain(suggestionOnlySentence);
  expect(text).not.toMatch(/\d/);

  // The rendered affordance: one card in the fix card's own recipe. The
  // sentence names the cause in the product's voice — a suggestion, never
  // a score, never a blame — and the link is the one-tap path to the
  // occasion's fix menu, where the ticks live. No render link: no render
  // exists to see.
  const screen = readFileSync(path.join(here, "verdict.svelte"), "utf8");
  const fork = screen.indexOf("{#if verdict.suggestionOnly}");
  const branch = screen.slice(fork, screen.indexOf("{:else}", fork));
  expect(branch).toContain('<div class="result-card">');
  expect(branch).toContain('<h3 class="hero-label">No fix yet</h3>');
  expect(branch).toContain('<p class="hero-fix">{suggestionOnlySentence}</p>');
  expect(branch).toContain('<a class="primary-link" href={occasionHash}>Choose the fixes to try</a>');
  expect(branch).not.toContain("renderHash");
  expect(suggestionOnlySentence).toBe("No fix is open to try yet; the look stands on its suggestions.");
  expect(suggestionOnlySentence).not.toMatch(/\d/);

  // The path binds the constant, and the shell routes that hash to the
  // occasion screen — the pins on both sides, so a swap on either fails a
  // test instead of a user.
  expect(occasionHash).toBe("#/occasion");
  const shell = readFileSync(path.join(here, "../../../app/app.svelte"), "utf8");
  expect(shell).toContain('hash === "/occasion"');
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
  expect(staticText).toContain("Dress code notes");
  expect(staticText).toContain("A fix");
  expect(staticText).toContain("Progress");
  expect(staticText).not.toContain("What the dress code still asks for");
  expect(staticText).not.toContain("Checked against the dress code");
  expect(staticText).not.toContain("Back");
  expect(staticText).not.toContain("Why");
  expect(screen).toContain("stageWord(");
  expect(screen).toContain("pausedSentence");
  expect(screen).toContain("{verdict.fix}");
  expect(screen).not.toContain("{verdict.why}");
  expect(screen).not.toContain(">Why<");
  expect(screen).toContain("{reason}");
  expect(screen).not.toContain("stream.current");
  expect(screen).not.toContain("stream.total");
  expect(screen).not.toContain("stream.reconnects");
  // The way back is the top bar's circular button, not an inline link.
  expect(screen).not.toContain('class="back"');
  expect(screen).not.toContain(">Back<");
  // The screen opens on the photograph, in every state the look reaches: the
  // arched comparator when the render stands, the selfie in the arched
  // mirror frame until it does. The pictures are read beside the job, not
  // after it.
  expect(screen).toContain("loadLook");
  expect(screen).toContain("{#if look?.render}");
  expect(screen).toContain("<Swipe before={look.selfieUrl} after={look.render.url} />");
  expect(screen).toContain('<div class="mirror-frame"><img src={look.selfieUrl} alt="Your selfie." /></div>');
  expect(markup).toContain("mirror-frame");
  expect(markup).not.toContain("lookId");
  expect(markup).not.toContain("jobId");

  // The live path: four words, the one in hand named as the step, the hero
  // carrying the fix, and the finished verdict never a dead end. Once the
  // look finishes the steps fold into their own drawer; while it runs the
  // stepper stands directly on the screen.
  expect(screen).toContain('<details class="disclosure stages-drawer">');
  expect(screen).toContain("<summary>Progress</summary>");
  expect(screen).toContain('{@render stages()}');
  expect(screen).toContain('<ol class="stages"');
  expect(screen).toContain("aria-current=");
  // The missed criteria fold away the same way, named by their summary, and
  // the hero carries the fix alone — the why sentence repeated reasons[0]
  // word for word, so it is gone.
  expect(screen).toContain('<details class="disclosure reasons-drawer">');
  expect(screen).toContain("<summary>Dress code notes</summary>");
  // One card language with the render screen: the fix and its way forward
  // are one cohesive Result Card on the shared recipe, the hero bare of
  // chrome inside it, and the crescent sliver gone from the file. This
  // screen holds no shop row and no verdict bar to add, so its card carries
  // the fix and the render link alone.
  expect(screen).toContain('<div class="result-card">');
  expect(screen).toContain('<div class="hero">');
  expect(screen).toContain('<h3 class="hero-label">A fix</h3>');
  expect(screen).toContain('<p class="hero-fix">{verdict.fix}</p>');
  expect(screen).toContain('<a class="primary-link" href={renderHash}>See the render</a>');
  expect(staticText).toContain("See the render");
  expect(markup).toContain("renderHash");
  // Two cards wear the result-card recipe now — the suggestion-only card
  // and the fix card — so the fix card's slice starts at its own markup,
  // the second one on the screen.
  const card = screen.slice(screen.lastIndexOf('<div class="result-card">'), screen.indexOf("</section>"));
  expect(card).toContain('<a class="primary-link" href={renderHash}>See the render</a>');
  expect(card.indexOf("hero-fix")).toBeLessThan(card.indexOf('class="primary-link"'));
  const heroStyles = screen.slice(screen.indexOf("<style>"));
  expect(heroStyles).toMatch(
    /\.result-card\s*\{[^}]*background: var\(--surface\)[^}]*border: 2px solid var\(--accent\)/,
  );
  expect(heroStyles).toMatch(/\.result-card\s*\{[^}]*border-radius: var\(--radius-control\)/);
  expect(heroStyles).toMatch(/\.result-card\s*\{[^}]*padding: 1rem/);
  expect(heroStyles).toMatch(/\.result-card\s*\{[^}]*display: flex/);
  expect(heroStyles).toMatch(/\.result-card\s*\{[^}]*flex-direction: column/);
  expect(heroStyles).toMatch(/\.result-card\s*\{[^}]*gap: 0\.85rem/);
  expect(screen).not.toContain("border-left: 3px solid var(--accent)");
  expect(heroStyles).not.toMatch(/\.hero\s*\{[^}]*background/);
  expect(heroStyles).not.toMatch(/\.hero\s*\{[^}]*border/);
  expect(heroStyles).toMatch(/\.primary-link\s*\{[^}]*color: var\(--on-accent\)/);
  expect(heroStyles).toMatch(/\.primary-link\s*\{[^}]*background: var\(--accent\)/);
  expect(heroStyles).toMatch(/\.stages-drawer > ol\s*\{[^}]*padding: 0\.65rem 0\.9rem 0\.85rem/);
  expect(heroStyles).toMatch(/\.reasons-drawer ul\s*\{[^}]*list-style: none/);
  expect(heroStyles).toMatch(/\.reasons-drawer ul\s*\{[^}]*padding: 0\.65rem 0\.9rem 0\.85rem/);
  // The mirror is the arch the capture screen holds: a clipped portrait box.
  expect(heroStyles).toMatch(/\.mirror-frame\s*\{[^}]*border-radius: 999px 999px var\(--radius-panel\) var\(--radius-panel\)/);
  expect(heroStyles).toMatch(/\.mirror-frame\s*\{[^}]*border: 2px solid var\(--line\)/);
  expect(heroStyles).toMatch(/\.mirror-frame\s*\{[^}]*overflow: hidden/);
  expect(heroStyles).toMatch(/\.mirror-frame img\s*\{[^}]*object-fit: cover/);
  // The pulse is motion the reader asked for, never one forced on them.
  expect(heroStyles).toMatch(
    /@media \(prefers-reduced-motion: no-preference\)\s*\{[\s\S]*?animation: stage-pulse/,
  );

  // The verdict is a hash branch of the app, not a footer text link: the
  // bottom blog row and its `See the verdict` link are gone with the tab-bar
  // shell, so what is true of `app.svelte` is the branch, not a label.
  const shell = readFileSync(path.join(here, "../../../app/app.svelte"), "utf8");
  expect(shell).toContain("isVerdictHash");
  expect(shell).toContain('route === "/look/verdict"');
  expect(shell).toContain("<Verdict />");
  expect(shell).not.toContain("See the verdict");
  expect(verdictHash).toBe("#/look/verdict");
});
