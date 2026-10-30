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

test("an event stream keeps its recorded body and the story media serves real photographs", async () => {
  installMock();
  const events = await fetch("/api/jobs/10b0000000000000000000000000000e/events");
  expect(events.headers.get("content-type")).toBe("text/event-stream");
  const text = await events.text();
  expect(text).toBe(disk("job-events-error.json").body);
  expect(readJobEvents(text).some((event) => event.name === "error")).toBe(true);

  const missing = await fetch("/media/00000000000000000000000000000000");
  expect(missing.status).toBe(404);
  expect(await missing.text()).toBe(disk("media-missing.json").body);

  // The recorded media ids serve the committed sample photographs: real
  // JPEG bytes, not the recorded empty body. The selfie is the first
  // visit, the render the finished look, the product the kept jhumkas.
  const photo = await fetch("/media/5e1f1e00000000000000000000000001");
  expect(photo.status).toBe(200);
  expect(photo.headers.get("content-type")).toBe("image/jpeg");
  const bytes = new Uint8Array(await photo.arrayBuffer());
  expect(bytes.byteLength).toBeGreaterThan(0);
  expect(bytes[0]).toBe(0xff);
  expect(bytes[1]).toBe(0xd8);

  for (const id of ["2e4de200000000000000000000000001", "c0a10600000000000000000000000001"]) {
    const media = await fetch(`/media/${id}`);
    expect(media.status).toBe(200);
    expect(media.headers.get("content-type")).toBe("image/jpeg");
    expect((await media.arrayBuffer()).byteLength).toBeGreaterThan(0);
  }

  await expect(api("/media/00000000000000000000000000000000")).rejects.toMatchObject({
    code: "http_error",
    status: 404,
  });
});

test("recall answers the off world until the session turns history on", async () => {
  installMock();
  const off = await api<Record<string, unknown>>("/api/history/recall?occasion=family-function");
  expect(off).toEqual(disk("recall-off.json").body);
  expect(lastFixtureFile).toBe("recall-off.json");

  await api("/api/history", { method: "PUT", body: JSON.stringify({ enabled: true }) });
  expect(lastFixtureFile).toBe("history-put.json");
  const on = await api<Record<string, unknown>>("/api/history/recall?occasion=family-function");
  expect(on).toEqual(disk("recall.json").body);
  expect(on.enabled).toBe(true);
  expect(on.line).toBe((disk("recall.json").body as { line: string }).line);

  // A fresh session, or a session that turned the switch back off, reads
  // the off world again.
  await api("/api/history", { method: "PUT", body: JSON.stringify({ enabled: false }) });
  const offAgain = await api<Record<string, unknown>>("/api/history/recall?occasion=family-function");
  expect(offAgain).toEqual(disk("recall-off.json").body);
  expect(lastFixtureFile).toBe("recall-off.json");
});
