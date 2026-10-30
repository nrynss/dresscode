import { ApiError } from "@nrynss/chaaya/keel";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { api } from "./client";
import { humanMessage, recordedSentences } from "./errors";
import { fixtureCode, loadFixtures } from "./fixtures";
import { installMock, pinAnswer } from "./mock";
import type { ApiFixture } from "./types";
import { eventError, readJobEvents, readJobStateError, readWireError } from "./wire";

const here = path.dirname(fileURLToPath(import.meta.url));
const apiDir = path.resolve(here, "../../../../testdata/api");
const errorsDir = path.join(apiDir, "errors");

const banned = /\d|%|https?:|\/\/|score|probab|percent|gender|attract|ratio/i;

function readJson(file: string): { body?: unknown; status?: number } {
  return JSON.parse(readFileSync(file, "utf8")) as { body?: unknown; status?: number };
}

afterEach(() => {
  installMock()();
});

test("every recorded error code arrives typed and renders a human sentence", async () => {
  installMock();
  const sentences = recordedSentences();
  const files = readdirSync(errorsDir).filter((name) => name.endsWith(".json")).sort();
  expect(files.length).toBeGreaterThan(0);

  const codes: string[] = [];
  for (const name of files) {
    const disk = readJson(path.join(errorsDir, name));
    const code = name.slice(0, -".json".length).replaceAll("-", "_");
    const typed = readWireError(JSON.stringify(disk.body));
    expect(typed?.code, name).toBe(code);
    codes.push(code);

    const sentence = humanMessage({ code, message: "wire phrase", detail: typed?.detail });
    expect(sentence, code).not.toBe("wire phrase");
    expect(sentence.endsWith("."), code).toBe(true);
    expect(sentence, code).not.toMatch(banned);
    expect(sentences[code], code).toBeTypeOf("string");

    const fixture = loadFixtures().find((item) => item.file === `errors/${name}`);
    expect(fixtureCode(fixture as ApiFixture)).toBe(code);
    if (typeof disk.status === "number" && disk.status >= 400 && fixture) {
      const route = `/api/__errors/${code}`;
      pinAnswer("GET", route, fixture);
      await expect(api(route)).rejects.toMatchObject({
        name: "ApiError",
        code,
        status: disk.status,
      });
      try {
        await api(route);
      } catch (error) {
        expect(error).toBeInstanceOf(ApiError);
        const thrown = error as ApiError;
        const shown = humanMessage({
          code: thrown.code,
          message: thrown.message,
          detail: thrown.detail,
          status: thrown.status,
        });
        expect(shown, code).not.toMatch(banned);
        expect(shown.endsWith("."), code).toBe(true);
        if (code === "rate_limited") expect(thrown.retryAfterSeconds).toBe(1);
        if (code === "send_limited") expect(thrown.retryAfterSeconds).toBe(30);
        if (code === "lease_refused") expect(thrown.detail.cap).toBe("fresh_selfies");
        if (code === "budget_refused") expect(thrown.detail.budget).toBe("youcam");
      }
    }
  }

  expect(Object.keys(sentences).sort()).toEqual(codes.sort());
});

test("lease, limit and invalid request use the detail without a number", () => {
  const cases = [
    { code: "lease_refused", detail: { cap: "fresh_selfies" }, text: "another selfie" },
    { code: "lease_refused", detail: { cap: "renders" }, text: "another render" },
    { code: "limit_exceeded", detail: { limit: "upload", max_bytes: 4194304 }, text: "too large" },
    { code: "limit_exceeded", detail: { limit: "owner" }, text: "no room" },
    { code: "invalid_request", detail: { field: "occasion", reason: "unknown occasion" }, text: "occasion" },
    { code: "invalid_request", detail: { field: "choice" }, text: "sign-in choice" },
  ];
  for (const item of cases) {
    const sentence = humanMessage({ code: item.code, detail: item.detail, message: "wire" });
    expect(sentence.toLowerCase()).toContain(item.text);
    expect(sentence).not.toMatch(banned);
  }
});

test("a job state error is the envelope member, and a stream error is wrapped again", () => {
  const state = readJson(path.join(apiDir, "job-error.json")).body;
  expect(readWireError(JSON.stringify((state as { error: unknown }).error))).toBeUndefined();
  expect(readJobStateError(state)?.code).toBe("no_face_in_photo");

  const stream = readJson(path.join(apiDir, "job-events-error.json")).body;
  expect(typeof stream).toBe("string");
  const found = readJobEvents(stream as string).map(eventError).find((item) => item !== undefined);
  expect(found?.code).toBe("no_face_in_photo");
  expect(humanMessage({ code: found?.code ?? "", message: found?.message })).toBe(
    "No face is visible in that photo. Try another.",
  );
});

test("renders paused and job failed are typed even though they are not HTTP failures", () => {
  for (const name of ["renders-paused.json", "job-failed.json"]) {
    const disk = readJson(path.join(errorsDir, name));
    expect(disk.status).toBeUndefined();
    const typed = readWireError(JSON.stringify(disk.body));
    const code = name.slice(0, -".json".length).replaceAll("-", "_");
    expect(typed?.code).toBe(code);
    expect(humanMessage({ code, message: typed?.message })).not.toMatch(banned);
  }
});
