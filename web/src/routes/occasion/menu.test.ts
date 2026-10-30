import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { installMock, pinAnswer, preferFixture } from "../../lib/api/mock";
import type { ApiFixture } from "../../lib/api/types";
import { listOccasions, readOccasion, readOptIn, readRecallLine, resetOccasionMenu, writeOptIn } from "./menu";

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

test("the occasion screen does not grade a person", () => {
  const banned = /\b(score|probability|percent|gender|attractiveness|ratio)\b/i;
  const screen = readFileSync(fileURLToPath(new URL("./occasion.svelte", import.meta.url)), "utf8");
  const menu = readFileSync(fileURLToPath(new URL("./menu.ts", import.meta.url)), "utf8");
  expect(screen).not.toMatch(banned);
  expect(menu).not.toMatch(banned);
});
