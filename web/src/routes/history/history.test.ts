import { ApiError } from "@nrynss/chaaya/keel";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { humanMessage } from "../../lib/api/errors";
import { installMock, lastFixtureFile, pinAnswer, preferFixture } from "../../lib/api/mock";
import {
  emptyVisitSentence,
  historyFromResponse,
  isLookVerdict,
  lookLine,
  lookVerdicts,
  looksFromResponse,
  nothingYetSentence,
  occasionsOf,
  openingSentence,
  openingVisitSentence,
  recallFromResponse,
  stillBeingFinishedSentence,
} from "./copy";
import { loadSecondVisit, readHistory, resetHistory, shownFailure, writeHistory } from "./flow";
import { historyHash, isHistoryHash } from "./route";

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../../..");
const apiDir = path.resolve(webRoot, "../testdata/api");

interface RecordedFile {
  method?: string;
  path?: string;
  request?: unknown;
  body?: unknown;
}

function disk(file: string): RecordedFile {
  return JSON.parse(readFileSync(path.join(apiDir, file), "utf8")) as RecordedFile;
}

/** The static words of a screen: its markup without the style block, the
 * expressions, and the tags. What a reader would see. */
function staticText(screen: string): string {
  const start = screen.indexOf("</script>");
  const markup = start < 0 ? screen : screen.slice(start);
  return markup
    .replace(/<style>[\s\S]*<\/style>/, "")
    .replace(/\{#[^}]*\}/g, " ")
    .replace(/\{:[^}]*\}/g, " ")
    .replace(/\{\/[^}]*\}/g, " ")
    .replace(/\{[^}]*\}/g, " ")
    .replace(/<[^>]+>/g, " ");
}

/** The words of style memory. They may arrive from the API while history is
 * on; they never sit in a screen's own markup, and with history off nothing
 * anywhere asks for them. */
const memoryWords = /\bmemory\b|\bremember(?:s|ing|ed)?\b|\brecall(?:s|ed|ing)?\b|jhumkas|second visit/i;

function svelteFiles(dir: string): string[] {
  const found: string[] = [];
  for (const name of readdirSync(dir)) {
    const abs = path.join(dir, name);
    if (statSync(abs).isDirectory()) found.push(...svelteFiles(abs));
    else if (abs.endsWith(".svelte")) found.push(abs);
  }
  return found;
}

interface Call {
  method: string;
  path: string;
  body: unknown;
}

function watchFetch(): { calls: Call[]; stop: () => void } {
  const calls: Call[] = [];
  const inner = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const url = new URL(raw, "http://dresscode.local");
    let body: unknown;
    if (typeof init?.body === "string" && init.body !== "") body = JSON.parse(init.body) as unknown;
    calls.push({ method, path: url.pathname + url.search, body });
    return inner(input, init);
  }) as typeof fetch;
  return {
    calls,
    stop() {
      globalThis.fetch = inner;
    },
  };
}

afterEach(() => {
  resetHistory();
  installMock()();
});

test("the route answers to its hash", () => {
  expect(historyHash).toBe("#/history");
  expect(isHistoryHash("#/history")).toBe(true);
  expect(isHistoryHash("/history")).toBe(true);
  expect(isHistoryHash("history")).toBe(true);
  expect(isHistoryHash("#/look/render")).toBe(false);
  expect(isHistoryHash("#/")).toBe(false);
});

test("the switch starts off and a fresh session's screen says nothing about memory", async () => {
  installMock();
  const state = await readHistory();
  expect(lastFixtureFile).toBe("history.json");
  expect(state.enabled).toBe(false);
  expect(state.notice).toBe((disk("history.json").body as { notice: string }).notice);

  // The recorded answer to recall with the switch off is the closed switch
  // and nothing else: no sentence, no fix ids.
  expect(disk("recall-off.json").body).toEqual({ enabled: false });

  // The words of memory never sit in the screen's own markup. The notice,
  // the recall sentence, and the looks in words all arrive from the API;
  // the markup names only the switch and the sections.
  const screen = readFileSync(path.join(here, "history.svelte"), "utf8");
  expect(staticText(screen), "history.svelte").not.toMatch(memoryWords);
  expect(staticText(screen)).toContain("Style history");
  const sentences = [
    openingSentence,
    openingVisitSentence,
    emptyVisitSentence,
    stillBeingFinishedSentence,
    nothingYetSentence,
  ].join(" ");
  expect(sentences).not.toMatch(memoryWords);

  // And no screen of the app carries them, while the switch is off.
  for (const file of svelteFiles(path.join(webRoot, "src"))) {
    expect(staticText(readFileSync(file, "utf8")), file).not.toMatch(memoryWords);
  }

  // With the switch off the looks still come — viewing what you have
  // already tried is free — and not one recall request is made, so not
  // one word of memory arrives anywhere.
  const seen = watchFetch();
  try {
    const visit = await loadSecondVisit();
    for (const group of visit.groups) expect(group.line).toBe("");
    expect(seen.calls.some((call) => call.path.includes("/api/history/recall"))).toBe(false);
  } finally {
    seen.stop();
  }
});

test("turning it on persists across the screens that read it", async () => {
  installMock();
  const seen = watchFetch();
  const written = await writeHistory(true);
  try {
    // The body sent is the body the recording carries, and the stored
    // answer is what the write returned.
    expect(seen.calls).toEqual([{ method: "PUT", path: "/api/history", body: { enabled: true } }]);
  } finally {
    seen.stop();
  }
  expect(lastFixtureFile).toBe("history-put.json");
  expect(disk("history-put.json").request).toEqual({ enabled: true });
  expect(written.enabled).toBe(true);
  expect(written.notice).toBe((disk("history-put.json").body as { notice: string }).notice);

  // A later read on re-entering the screen reports the stored switch and
  // asks nothing again, per the API's shape: the PUT stored it.
  const again = watchFetch();
  try {
    const state = await readHistory();
    expect(state.enabled).toBe(true);
    expect(state.notice).toBe(written.notice);
    expect(again.calls).toEqual([]);
  } finally {
    again.stop();
  }

  // The reader of the second visit sees the same switch: where the fresh
  // session refused to ask, now it reads the past looks.
  // The mock's story now follows the session's switch, so this reader pin
  // names the file it reads: the off body, whatever the session stored.
  preferFixture("GET", "/api/history/recall?occasion=family-function", "recall-off.json");
  const visit = await loadSecondVisit();
  expect(visit.groups).toHaveLength(1);
  expect(visit.groups[0]?.occasion).toEqual({ id: "family-function", name: "Family function" });
  // The reader honours a body's own switch over the session's: the off
  // body is pinned, so no sentence comes out.
  expect(visit.groups[0]?.line).toBe("");
});

test("with history on, the second visit plays the recorded story", async () => {
  installMock();
  await writeHistory(true);
  // The recorded world with the switch on: recall answers with the sentence
  // it kept for this occasion.
  preferFixture("GET", "/api/history/recall?occasion=family-function", "recall.json");
  const seen = watchFetch();
  const visit = await loadSecondVisit();
  seen.stop();
  // Recall was asked for the occasion, and the sentence it answered with is
  // the one the recording keeps; the default answer is still the off world,
  // so only the recorded on body can produce it.
  expect(seen.calls.filter((call) => call.path === "/api/history/recall?occasion=family-function")).toEqual([
    { method: "GET", path: "/api/history/recall?occasion=family-function", body: undefined },
  ]);

  expect(visit.groups).toHaveLength(1);
  const group = visit.groups[0]!;
  expect(group.occasion).toEqual({ id: "family-function", name: "Family function" });
  expect(group.line).toBe((disk("recall.json").body as { line: string }).line);
  // The sentence names the occasion and the fix that was kept, from the
  // recorded look.
  expect(group.line).toContain("family function");
  expect(group.line).toContain("gold jhumkas");

  // The recorded fix ids ride along for the picker and reach no markup.
  const recall = recallFromResponse(disk("recall.json").body, "family-function");
  expect(recall.kept).toEqual(["family-gold-jhumkas"]);
  expect(recall.dismissed).toEqual(["family-red-lip"]);
  const screen = readFileSync(path.join(here, "history.svelte"), "utf8");
  expect(screen).toContain("{group.line}");
  expect(screen).not.toContain("family-gold-jhumkas");
  expect(screen).not.toContain("family-red-lip");

  // The look under the sentence is the recorded story look, shown in words.
  const recorded = (disk("looks.json").body as { looks: { id: string; words: string }[] }).looks[0]!;
  expect(group.looks).toEqual([
    { id: recorded.id, verdict: "", ready: true, words: recorded.words },
  ]);
  expect(lookLine(group.looks[0]!)).toBe(recorded.words);
  expect(screen).toContain("{lookLine(look)}");
  expect(screen).toContain("{group.occasion.name}");
});

test("past looks group by occasion", async () => {
  installMock();
  await writeHistory(true);
  preferFixture("GET", "/api/history/recall?occasion=family-function", "recall.json");
  // A second occasion with a look, in the recorded item shape, beside the
  // recorded empty answer for an occasion memory has not met yet.
  pinAnswer("GET", "/api/looks?occasion=date", {
    file: "looks-date.json",
    method: "GET",
    path: "/api/looks?occasion=date",
    status: 200,
    body: {
      looks: [
        {
          id: "100c0000000000000000000000000003",
          occasion: "date",
          verdict: "kept",
          ready: true,
          fix: "",
          words: "a plain watch",
        },
      ],
    },
  });

  const visit = await loadSecondVisit();
  // One group per occasion that has looks, in the occasions' own order.
  expect(visit.groups.map((group) => group.occasion.id)).toEqual(["date", "family-function"]);
  expect(visit.groups.map((group) => group.occasion.name)).toEqual(["Date", "Family function"]);

  // The date occasion has no memory yet: the recorded empty recall, so no
  // sentence, and its look still listed with its verdict word.
  expect(visit.groups[0]?.line).toBe("");
  expect(visit.groups[0]?.looks).toEqual([
    { id: "100c0000000000000000000000000003", verdict: "kept", ready: true, words: "a plain watch" },
  ]);
  expect(lookLine(visit.groups[0]!.looks[0]!)).toBe("a plain watch");

  // The family-function group keeps the story's sentence and its look.
  expect(visit.groups[1]?.line).toContain("gold jhumkas");
  expect(visit.groups[1]?.looks).toHaveLength(1);

  // Occasions with nothing recorded here carry no group at all.
  expect(visit.groups.some((group) => group.occasion.id === "interview")).toBe(false);
  expect(visit.groups.some((group) => group.occasion.id === "festival")).toBe(false);
  expect(visit.groups.some((group) => group.occasion.id === "funeral")).toBe(false);
});

test("a visit that cannot be read fails through the usual sentence", async () => {
  installMock();
  await writeHistory(true);
  pinAnswer("GET", "/api/looks?occasion=date", {
    file: "looks-date-error",
    method: "GET",
    path: "/api/looks?occasion=date",
    status: 500,
    body: disk("errors/internal-error.json").body,
  });

  let caught: unknown;
  try {
    await loadSecondVisit();
  } catch (error: unknown) {
    caught = error;
  }
  expect(caught).toBeInstanceOf(ApiError);
  expect(humanMessage(shownFailure(caught))).toBe("Something went wrong on the way. Try again.");

  // A failure that is not a wire envelope is a sentence too, and the
  // screen takes both through the same notice.
  expect(humanMessage(shownFailure(new Error("The past looks could not be read.")))).toBe(
    "The past looks could not be read.",
  );
  expect(humanMessage(shownFailure(new ApiError("no", "not_found", 404)))).toBe("That is not available.");
  const screen = readFileSync(path.join(here, "history.svelte"), "utf8");
  expect(screen).toContain("<ErrorNotice");
  expect(screen).toContain("shownFailure");
});

test("no digit anywhere and no id on the screen", () => {
  const screen = readFileSync(path.join(here, "history.svelte"), "utf8");
  const markup = staticText(screen);
  expect(markup).not.toMatch(/\d/);
  expect(markup).toContain("Style history");
  // The notice folds into a disclosure closed by default, and the redundant
  // subheading above the occasion groups is gone; the groups carry the
  // occasion names themselves.
  expect(markup).toContain("How style history works");
  expect(markup).not.toContain("Past looks");
  // The pattern, not the exact `<details open` prefix, so a tag that carries
  // the attribute after its class cannot slip past. The drawer class is the
  // marker the mounted open state waits on, so the pin names it too.
  expect(screen).not.toMatch(/<details\b[^>]*\bopen\b/);
  expect(screen).toContain('class="disclosure notice-drawer"');
  // The heading and the switch share one bar, and the switch's own text
  // hides inside its label, so the control still carries its name.
  expect(screen).toContain('class="history-bar"');
  expect(screen).toContain('<span class="sr-only">Style history</span>');
  expect(markup).toContain("Back");
  // The ids and counts stay keys, never words on the screen.
  expect(markup).not.toContain("lookId");
  expect(markup).not.toContain("jobId");
  expect(screen).not.toContain("family-gold-jhumkas");
  expect(screen).not.toContain("family-red-lip");

  const words = [
    openingSentence,
    openingVisitSentence,
    emptyVisitSentence,
    stillBeingFinishedSentence,
    nothingYetSentence,
  ].join(" ");
  expect(words).not.toMatch(/\d/);

  const recorded = [
    (disk("history.json").body as { notice: string }).notice,
    (disk("history-put.json").body as { notice: string }).notice,
    (disk("recall.json").body as { line: string }).line,
    (disk("looks.json").body as { looks: { words: string }[] }).looks[0]!.words,
  ].join(" ");
  expect(recorded).not.toMatch(/\d/);
  for (const occasion of (disk("occasions.json").body as { occasions: { name: string }[] }).occasions) {
    expect(occasion.name).not.toMatch(/\d/);
  }

  // The wiring is the twice-accepted pattern, re-homed with the tab-bar
  // shell. The route branch stays in app.svelte, and isHistoryHash is the
  // symbol that decides it there, so the import pin reads that name now.
  // The visible way in moved with the deleted footer into the shell's fixed
  // tab bar, so the link and label pins read shell.svelte. Both are pinned
  // as the markup a swap would have to survive: the bare words `historyHash`
  // and `History` sit in the shell's import list and in isHistoryHash's own
  // name, so a plain-word check could never see the href or a rename go.
  const app = readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8");
  expect(app).toContain("isHistoryHash");
  expect(app).toContain('route === "/history"');
  expect(app).toContain("<History />");
  const shell = readFileSync(path.join(webRoot, "src/app/shell.svelte"), "utf8");
  expect(shell).toContain("href={historyHash}");
  expect(shell).toContain("<span>History</span>");
});

test("a body that cannot be read is refused", () => {
  expect(() => historyFromResponse({})).toThrow();
  expect(() => historyFromResponse({ enabled: true })).toThrow();
  expect(historyFromResponse({ enabled: false, notice: "Style history is off." })).toEqual({
    enabled: false,
    notice: "Style history is off.",
  });

  // A closed switch carries no memory, and a word of memory beside one is
  // refused, so it cannot reach the screen.
  expect(recallFromResponse({ enabled: false }, "family-function")).toEqual({
    enabled: false,
    occasion: "family-function",
    line: "",
    kept: [],
    dismissed: [],
  });
  expect(() => recallFromResponse({ enabled: false, line: "Last family function." }, "family-function")).toThrow();
  expect(() => recallFromResponse({ enabled: false, occasion: "family-function" }, "family-function")).toThrow();
  expect(() => recallFromResponse({}, "family-function")).toThrow();
  expect(() => recallFromResponse({ enabled: "yes" }, "family-function")).toThrow();
  // Another occasion's memory is not this answer.
  expect(() =>
    recallFromResponse({ enabled: true, occasion: "date", line: "", kept: [], dismissed: [] }, "family-function"),
  ).toThrow();
  // An occasion with no memory yet, while history is on, is the recorded
  // empty shape.
  expect(recallFromResponse(disk("recall-empty.json").body, "date")).toEqual({
    enabled: true,
    occasion: "date",
    line: "",
    kept: [],
    dismissed: [],
  });

  expect(() => looksFromResponse({}, "date")).toThrow();
  expect(() =>
    looksFromResponse({ looks: [{ id: "x", occasion: "date", verdict: "seen", ready: true, words: "" }] }, "date"),
  ).toThrow();
  expect(() =>
    looksFromResponse({ looks: [{ id: "x", occasion: "festival", verdict: "", ready: true, words: "w" }] }, "date"),
  ).toThrow();

  expect(lookVerdicts).toEqual(["kept", "dismissed", "bought"]);
  expect(isLookVerdict("kept")).toBe(true);
  expect(isLookVerdict("seen")).toBe(false);

  expect(lookLine({ id: "x", verdict: "", ready: false, words: "" })).toBe(stillBeingFinishedSentence);
  expect(lookLine({ id: "x", verdict: "", ready: true, words: "" })).toBe(nothingYetSentence);
  expect(lookLine({ id: "x", verdict: "", ready: true, words: "gold jhumkas" })).toBe("gold jhumkas");

  expect(occasionsOf(disk("occasions.json").body)).toEqual([
    { id: "date", name: "Date" },
    { id: "family-function", name: "Family function" },
    { id: "interview", name: "Interview" },
    { id: "festival", name: "Festival" },
    { id: "funeral", name: "Funeral" },
  ]);
  expect(() => occasionsOf({})).toThrow();

  // And the recorded story list reads back whole.
  const looks = looksFromResponse(disk("looks.json").body, "family-function");
  expect(looks).toEqual([{ id: (disk("looks.json").body as { looks: { id: string }[] }).looks[0]!.id, verdict: "", ready: true, words: "gold jhumkas" }]);
});
