import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { api } from "./client";
import { loadFixtures } from "./fixtures";
import { installMock, lastCredentials, lastFixtureFile, preferFixture } from "./mock";
import { mockEnabled } from "./mode";
import { readJobEvents } from "./wire";

const apiDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../../testdata/api");

function walk(dir: string, rel = ""): string[] {
  const found: string[] = [];
  for (const name of readdirSync(dir)) {
    const abs = path.join(dir, name);
    const next = rel ? `${rel}/${name}` : name;
    if (statSync(abs).isDirectory()) found.push(...walk(abs, next));
    else if (name.endsWith(".json")) found.push(next);
  }
  return found;
}

function disk(file: string): { method?: string; path?: string; status?: number; body?: unknown; headers?: Record<string, string> } {
  return JSON.parse(readFileSync(path.join(apiDir, file), "utf8")) as {
    method?: string;
    path?: string;
    status?: number;
    body?: unknown;
    headers?: Record<string, string>;
  };
}

afterEach(() => {
  installMock()();
});

test("mock mode is the dev default and serves every recorded file", async () => {
  expect(mockEnabled()).toBe(true);
  installMock();
  expect(loadFixtures().map((item) => item.file).sort()).toEqual(walk(apiDir).sort());

  const sessionFile = disk("session.json");
  const session = await api<unknown>("/api/session");
  expect(session).toEqual(sessionFile.body);
  expect(lastCredentials).toBe("include");

  const occasions = await api<{ occasions: unknown[] }>("/api/occasions");
  expect(occasions).toEqual(disk("occasions.json").body);
  expect(occasions.occasions).toHaveLength(5);
});

test("the story pick is the finished look and history stays off", async () => {
  installMock();
  const look = await api<{ ready: boolean }>("/api/looks/100c0000000000000000000000000001");
  expect(look).toEqual(disk("look.json").body);
  expect(look.ready).toBe(true);

  const recall = await api<Record<string, unknown>>("/api/history/recall?occasion=family-function");
  expect(recall).toEqual({ enabled: false });
  expect(recall.line).toBeUndefined();

  preferFixture("GET", "/api/looks/100c0000000000000000000000000001", "look-pending.json");
  const pending = await api<{ ready: boolean }>("/api/looks/100c0000000000000000000000000001");
  expect(pending).toEqual(disk("look-pending.json").body);
  expect(pending.ready).toBe(false);
});

test("verify and verdict follow the request body", async () => {
  installMock();
  const kept = await api<{ verdict: string }>("/api/looks/100c0000000000000000000000000001/verdict", {
    method: "PUT",
    body: JSON.stringify({ verdict: "kept" }),
  });
  expect(kept.verdict).toBe("kept");

  const bought = await api<{ verdict: string }>("/api/looks/100c0000000000000000000000000001/verdict", {
    method: "PUT",
    body: JSON.stringify({ verdict: "bought" }),
  });
  expect(bought.verdict).toBe("bought");

  const switched = await api("/api/session/code/verify", {
    method: "POST",
    body: JSON.stringify({ address: "person@example.com", code: "000000", choice: "switch" }),
  });
  expect(lastFixtureFile).toBe("session-verify-switch.json");
  expect(switched).toEqual(disk("session-verify-switch.json").body);

  await api("/api/session/code/verify", {
    method: "POST",
    body: JSON.stringify({ address: "person@example.com", code: "000000", choice: "" }),
  });
  expect(lastFixtureFile).toBe("session-verify.json");
});

test("an event stream and a private media miss keep their recorded bodies", async () => {
  installMock();
  const events = await fetch("/api/jobs/10b0000000000000000000000000000e/events");
  expect(events.headers.get("content-type")).toBe("text/event-stream");
  const text = await events.text();
  expect(text).toBe(disk("job-events-error.json").body);
  expect(readJobEvents(text).some((event) => event.name === "error")).toBe(true);

  const missing = await fetch("/media/00000000000000000000000000000000");
  expect(missing.status).toBe(404);
  expect(await missing.text()).toBe(disk("media-missing.json").body);

  const photo = await fetch("/media/5e1f1e00000000000000000000000001");
  expect(photo.status).toBe(200);
  expect(photo.headers.get("content-type")).toBe("image/jpeg");
  expect((await photo.arrayBuffer()).byteLength).toBe(0);

  await expect(api("/media/00000000000000000000000000000000")).rejects.toMatchObject({
    code: "http_error",
    status: 404,
  });
});
