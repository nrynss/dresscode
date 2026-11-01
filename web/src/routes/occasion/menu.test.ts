import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { installMock, pinAnswer, preferFixture } from "../../lib/api/mock";
import type { ApiFixture } from "../../lib/api/types";
import { occasionIcons } from "./icons";
import { fixChipLabel, listOccasions, readOccasion, readOptIn, readRecallLine, resetOccasionMenu, writeOptIn } from "./menu";
import type { OccasionFix } from "./types";

interface Call {
  method: string;
  path: string;
  body: unknown;
}

afterEach(() => {
  resetOccasionMenu();
  installMock()();
});

function pinOptIn(id: string, method: "GET" | "PUT", ticked: string[]): void {
  const path = `/api/occasions/${id}/opt-in`;
  const fixture: ApiFixture = {
    file: `${id}-opt-in-${method.toLowerCase()}`,
    method,
    path,
    status: 200,
    body: { occasion: id, ticked },
  };
  pinAnswer(method, path, fixture);
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
    calls.push({ method, path: url.pathname, body });
    return inner(input, init);
  }) as typeof fetch;
  return {
    calls,
    stop() {
      globalThis.fetch = inner;
    },
  };
}

test("the menu saves and reloads per occasion", async () => {
  installMock();
  const family = ["family-short-beard", "family-gold-jhumkas"];
  const date = ["date-nude-lip", "date-short-beard"];
  const recordedFamily = ["family-gold-jhumkas", "family-red-lip", "family-festive-top"];
  expect(family).not.toEqual(recordedFamily);
  pinOptIn("date", "GET", []);
  pinOptIn("date", "PUT", ["date-open-collar"]);
  pinOptIn("festival", "GET", []);
  pinOptIn("festival", "PUT", ["festival-red-lip"]);

  expect(await readOptIn("family-function")).toEqual([]);
  expect(await readOptIn("date")).toEqual([]);
  expect(await readOptIn("festival")).toEqual([]);

  const seen = watchFetch();
  try {
    expect(await writeOptIn("family-function", family)).toEqual(family);
    expect(await writeOptIn("date", date)).toEqual(date);
    expect(await writeOptIn("festival", [])).toEqual([]);
    expect(seen.calls).toEqual([
      { method: "PUT", path: "/api/occasions/family-function/opt-in", body: { ticked: family } },
      { method: "PUT", path: "/api/occasions/date/opt-in", body: { ticked: date } },
      { method: "PUT", path: "/api/occasions/festival/opt-in", body: { ticked: [] } },
    ]);
  } finally {
    seen.stop();
  }

  const again = watchFetch();
  try {
    expect(await readOptIn("date")).toEqual(date);
    expect(await readOptIn("festival")).toEqual([]);
    expect(await readOptIn("family-function")).toEqual(family);
    expect(again.calls).toEqual([]);
  } finally {
    again.stop();
  }
});

test("a missed write does not replace another occasion", async () => {
  installMock();
  const family = ["family-tie-hair"];
  await writeOptIn("family-function", family);
  await expect(writeOptIn("interview", ["interview-nude-lip"])).rejects.toMatchObject({ code: "not_found" });
  await expect(readOptIn("interview")).rejects.toMatchObject({ code: "not_found" });
  expect(await readOptIn("family-function")).toEqual(family);
});

test("occasions stay in product order and the menu lists every fix", async () => {
  installMock();
  expect(await listOccasions()).toEqual([
    { id: "date", name: "Date" },
    { id: "family-function", name: "Family function" },
    { id: "interview", name: "Interview" },
    { id: "festival", name: "Festival" },
    { id: "funeral", name: "Funeral" },
  ]);

  const family = await readOccasion("family-function");
  expect(family.name).toBe("Family function");
  expect(family.fixes.map((fix) => fix.id)).toEqual([
    "family-gold-jhumkas",
    "family-red-lip",
    "family-festive-top",
    "family-tie-hair",
    "family-short-beard",
    "family-closed-smile",
  ]);
  expect(family.fixes.map((fix) => fix.changes)).toContain("Shapes a short, even beard.");
  expect(family.fixes.map((fix) => fix.changes)).toContain("Paints the lip red.");
});

test("the past-visit line answers the session's switch and refuses what is not recorded", async () => {
  installMock();
  // A fresh session's switch is off, so the recorded answer is the closed
  // one: the line comes back empty and nothing else arrives.
  expect(await readRecallLine("family-function")).toBe("");

  // With the switch on, the recorded answer is the line the visit kept.
  preferFixture("GET", "/api/history/recall?occasion=family-function", "recall.json");
  const line = await readRecallLine("family-function");
  expect(line).toContain("gold jhumkas");

  // An occasion the story has not met answers not_found, which the screen
  // reads as no line for now rather than a broken menu.
  await expect(readRecallLine("interview")).rejects.toMatchObject({ code: "not_found" });
});

test("every recorded fix carries its compact pill label", async () => {
  installMock();
  const expected: Record<string, string> = {
    "date-open-collar": "Open-collar shirt",
    "date-tie-hair": "Hair tied back",
    "date-short-beard": "Short, even beard",
    "date-nude-lip": "Even nude lip",
    "date-closed-smile": "Calm smile",
    "family-gold-jhumkas": "Gold jhumkas",
    "family-red-lip": "Red lip",
    "family-festive-top": "Festive mandarin top",
    "family-tie-hair": "Hair tied back",
    "family-short-beard": "Short, even beard",
    "family-closed-smile": "Calm smile",
    "interview-buttoned-shirt": "Buttoned shirt",
    "interview-tie-hair": "Hair tied back",
    "interview-short-hair": "Short, neat hair",
    "interview-short-beard": "Short, even beard",
    "interview-nude-lip": "Quiet nude lip",
    "interview-closed-smile": "Calm smile",
    "festival-festive-top": "Festive mandarin top",
    "festival-gold-jhumkas": "Gold jhumkas",
    "festival-gold-chain": "Gold chain",
    "festival-red-lip": "Red lip",
    "festival-tie-hair": "Hair tied back",
    "festival-full-beard": "Full, even beard",
    "festival-closed-smile": "Calm smile",
    "funeral-dark-shirt": "Dark buttoned shirt",
    "funeral-nude-lip": "Quiet nude lip",
    "funeral-tie-hair": "Hair tied back",
    "funeral-short-beard": "Short, close beard",
  };

  // Every fix the recorded menu carries maps to its pill label through the
  // recorded envelope, the way the screen reads it.
  const family = await readOccasion("family-function");
  expect(family.fixes.map((fix) => fixChipLabel(fix))).toEqual(
    family.fixes.map((fix) => expected[fix.id]),
  );

  // Every fix the product files record maps too, and no label carries a
  // digit or a banned grading word.
  const banned = /\b(score|probability|percent|gender|attractiveness|ratio)\b/i;
  const dir = fileURLToPath(new URL("../../../../testdata/occasions/", import.meta.url));
  for (const file of ["date.json", "family-function.json", "interview.json", "festival.json", "funeral.json"]) {
    const body = JSON.parse(readFileSync(`${dir}${file}`, "utf8")) as { fixes: { id: string; changes: string }[] };
    for (const fix of body.fixes) {
      const pill: OccasionFix = { id: fix.id, changes: fix.changes, criteria: [] };
      expect(fixChipLabel(pill), fix.id).toBe(expected[fix.id]);
      expect(expected[fix.id]).not.toMatch(/\d/);
      expect(expected[fix.id]).not.toMatch(banned);
    }
  }

  // A fix the table has not met falls back to the recorded sentence.
  expect(fixChipLabel({ id: "gala-new-piece", changes: "Adds something new.", criteria: [] })).toBe(
    "Adds something new.",
  );
});

test("the occasion screen does not grade a person", () => {
  const banned = /\b(score|probability|percent|gender|attractiveness|ratio)\b/i;
  const screen = readFileSync(fileURLToPath(new URL("./occasion.svelte", import.meta.url)), "utf8");
  const menu = readFileSync(fileURLToPath(new URL("./menu.ts", import.meta.url)), "utf8");
  expect(screen).not.toMatch(banned);
  expect(menu).not.toMatch(banned);
});

test("every occasion tile carries its drawn icon", () => {
  expect(Object.keys(occasionIcons).sort()).toEqual([
    "date",
    "family-function",
    "festival",
    "funeral",
    "interview",
  ]);
  for (const [id, paths] of Object.entries(occasionIcons)) {
    expect(paths.length, `${id} draws a figure`).toBeGreaterThan(0);
    for (const d of paths) {
      expect(d.startsWith("M"), `${id} draws on the 24-box`).toBe(true);
      expect(d, `${id} draws no fill`).not.toContain("fill");
    }
  }
});

test("the front door carries the short copy, the icon tiles and the clean selection border", () => {
  const screen = readFileSync(fileURLToPath(new URL("./occasion.svelte", import.meta.url)), "utf8");
  const app = readFileSync(fileURLToPath(new URL("../../app/app.svelte", import.meta.url)), "utf8");

  // The shortened front-door copy, verbatim — these strings go through the
  // writing review.
  expect(screen).toContain("<legend>Occasion</legend>");
  expect(screen).toContain("<legend>Fixes to try</legend>");
  expect(screen).toContain(">Continue</a>");
  expect(app).toContain("<summary>Sample looks</summary>");
  expect(screen).not.toContain("Choose an occasion");
  expect(screen).not.toContain("Fixes you are open to");
  expect(screen).not.toContain("Bring the look");
  expect(app).not.toContain("Try a sample look");

  // The tile: an icon badge drawn per occasion, and the picked tile bounded
  // by a clean full accent border — not the crescent sliver on one side.
  expect(screen).toContain('class="occ-icon" aria-hidden="true"');
  expect(screen).toMatch(/fieldset\.cards label:has\(input:checked\)\s*\{[^}]*border:\s*2px solid var\(--accent\)/);
  expect(screen).not.toContain("border-left: 3px solid var(--accent)");

  // The CTA spans the column with its label centred: the declaration the
  // browser-backed guard measures the rendering of.
  expect(screen).toMatch(/a\.bring\s*\{[^}]*display:\s*flex;\s*width:\s*100%;\s*justify-content:\s*center;/);

  // The sample buttons' face thumbnails name only media ids the mock really
  // serves, so no thumbnail points at a path that answers nothing. The
  // served set is read from `web/vite.config.ts`'s sample-media map — the
  // one table the dev middleware and the mock both read — so the pin cannot
  // drift from what actually answers.
  const viteConfig = readFileSync(fileURLToPath(new URL("../../../vite.config.ts", import.meta.url)), "utf8");
  const served = [...viteConfig.matchAll(/"(\/media\/[0-9a-f]+)":\s*"[^"]+"/g)].map((match) => match[1]);
  const named = [...app.matchAll(/\/media\/[0-9a-f]+/g)].map((match) => match[0]);
  expect(named.length, "the avatar table names served media paths").toBeGreaterThan(0);
  expect(served.length, "the sample-media map is read").toBeGreaterThan(0);
  for (const path of named) {
    expect(served, `${path} is served`).toContain(path);
  }
});
