import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { render } from "svelte/server";
import { afterEach, expect, test } from "vitest";
import { loadFixtures } from "../../lib/api/fixtures";
import { installMock, lastFixtureFile, pinAnswer, resetMockChoices } from "../../lib/api/mock";
import type { ApiFixture } from "../../lib/api/types";
import { readJobEvents } from "../../lib/api/wire";
import Account from "./account.svelte";
import {
  deleteAccount,
  deleteHistory,
  deleteLook,
  deletePhoto,
  deletionLines,
  listDeletableLooks,
  openDeletionStream,
  requestCode,
  verifySignIn,
} from "./flow";
import { accountSentences, privacyCopy, privacySentences } from "./privacy";

const here = path.dirname(fileURLToPath(import.meta.url));
const apiDir = path.resolve(here, "../../../../testdata/api");
const productPath = path.resolve(here, "../../../../product.md");

function disk(name: string): { path?: string; body?: unknown } {
  return JSON.parse(readFileSync(path.join(apiDir, name), "utf8")) as { path?: string; body?: unknown };
}

function jobId(name: string): string {
  const body = disk(name).body;
  if (typeof body !== "object" || body === null || !("job_id" in body)) throw new Error(name);
  const id = (body as { job_id?: unknown }).job_id;
  if (typeof id !== "string") throw new Error(name);
  return id;
}

function fixture(file: string): ApiFixture {
  const found = loadFixtures().find((item) => item.file === file);
  if (!found) throw new Error(file);
  return found;
}

function plain(markdown: string): string {
  return markdown.replaceAll("**", "");
}

function visible(html: string): string {
  return html
    .replaceAll("&quot;", '"')
    .replaceAll("&#34;", '"')
    .replaceAll("&#39;", "'")
    .replaceAll("&amp;", "&");
}

afterEach(() => {
  installMock()();
});

test("the privacy text is the product's sentences", () => {
  const product = readFileSync(productPath, "utf8");
  const text = plain(product);
  const copy = privacyCopy(product);
  const shown = [copy.heading, copy.cloudflare, ...privacySentences(product), ...Object.values(accountSentences(product))];
  expect(privacySentences()).toEqual(privacySentences(product));
  expect(accountSentences()).toEqual(accountSentences(product));
  for (const sentence of shown) {
    expect(text, sentence).toContain(sentence);
  }
  expect(copy.youcam.every((sentence) => sentence.startsWith("YouCam"))).toBe(true);
  expect(copy.clef.some((sentence) => sentence.startsWith("Clef"))).toBe(true);
  expect(copy.cloudflare.startsWith("Cloudflare Workers AI")).toBe(true);

  const body = visible(render(Account).body);
  for (const sentence of shown) {
    expect(body, sentence).toContain(sentence);
  }
  expect(body).not.toMatch(/\d+\s*\/\s*\d+|%/);
});

test("account deletion shows progress to the end on mock data", async () => {
  installMock();
  const previous = globalThis.window;
  globalThis.window = globalThis as unknown as Window & typeof globalThis;
  const id = jobId("delete-account.json");
  const stream = openDeletionStream(id);
  try {
    await requestCode("person@example.com");
    expect(lastFixtureFile).toBe("session-code.json");
    const bodies: string[] = [];
    const mocked = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (typeof init?.body === "string") bodies.push(init.body);
      return mocked(input, init);
    }) as typeof fetch;
    expect(await deleteAccount("person@example.com", "000000")).toBe(id);
    expect(lastFixtureFile).toBe("delete-account.json");
    expect(bodies).toContain(JSON.stringify({ address: "person@example.com", code: "000000" }));

    stream.attach(() => {});
    const start = Date.now();
    while (stream.status !== "done") {
      if (stream.connection === "failed") throw new Error(`stream failed with ${String(stream.events.length)} events`);
      if (Date.now() - start > 2000) {
        throw new Error(`timed out in ${stream.connection} with ${String(stream.events.length)} events`);
      }
      await new Promise((resolve) => setTimeout(resolve, 15));
    }
    const lines = deletionLines(stream.events);
    const recorded = deletionLines(readJobEvents(disk("job-events-erase.json").body as string));
    expect(lines).toEqual(recorded);
    expect(lines).toEqual([
      "Deleting the account.",
      "Every stored part is confirmed and your sign-in is removed.",
      "Every part has confirmed.",
    ]);
    expect(lines.at(-1)).toBe("Every part has confirmed.");
    expect(lines.join("\n")).not.toMatch(/\d|%|\//);
    expect(lines.join("\n")).not.toContain("a11ce0000000000000000000000000a1");
  } finally {
    stream.close();
    globalThis.window = previous;
  }
});

test("an erase stream does not show a count", () => {
  const lines = deletionLines([
    {
      id: 1,
      name: "progress",
      jobId: "de1e7e00000000000000000000000001",
      stage: "erase",
      current: 1,
      total: 2,
    },
    { id: 2, name: "done", jobId: "de1e7e00000000000000000000000001", status: "done" },
  ]);
  expect(lines).toEqual(["Deleting.", "Every target has confirmed."]);
  expect(lines.join(" ")).not.toMatch(/\d/);
});

test("switch repeats the same code and does not send another choice", async () => {
  installMock();
  const seen: { address?: string; choice?: string; code?: string }[] = [];
  const mocked = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    if (typeof init?.body === "string") {
      seen.push(JSON.parse(init.body) as { address?: string; choice?: string; code?: string });
    }
    return mocked(input, init);
  }) as typeof fetch;

  pinAnswer("POST", "/api/session/code/verify", fixture("errors/guest-data-conflict.json"));
  expect(await verifySignIn("person@example.com", "000000", "")).toEqual({ ok: false, conflict: true });
  resetMockChoices();
  expect(await verifySignIn("person@example.com", "000000", "switch")).toEqual({ ok: true, switched: true });
  expect(lastFixtureFile).toBe("session-verify-switch.json");
  expect(seen).toEqual([
    { address: "person@example.com", code: "000000", choice: "" },
    { address: "person@example.com", code: "000000", choice: "switch" },
  ]);
  expect(JSON.stringify(seen)).not.toContain("merge");
});

test("a new address verifies with an empty choice", async () => {
  installMock();
  expect(await verifySignIn("person@example.com", "000000", "")).toEqual({ ok: true, switched: false });
  expect(lastFixtureFile).toBe("session-verify.json");
});

test("photo, look, and history deletion follow the recorded paths", async () => {
  installMock();
  const looks = await listDeletableLooks();
  expect(looks).toEqual([
    {
      id: "100c0000000000000000000000000001",
      label: "Family function: gold jhumkas",
      selfieId: "5e1f1e00000000000000000000000001",
    },
  ]);
  const look = looks[0];
  if (!look?.selfieId) throw new Error("missing selfie");
  expect(await deleteLook(look.id)).toBe(jobId("delete-look.json"));
  expect(lastFixtureFile).toBe("delete-look.json");
  expect(await deletePhoto(look.selfieId)).toBe(jobId("delete-photo.json"));
  expect(lastFixtureFile).toBe("delete-photo.json");
  expect(await deleteHistory()).toBe(jobId("delete-history.json"));
  expect(lastFixtureFile).toBe("delete-history.json");
});

test("the shell link opens the account route", () => {
  const shell = readFileSync(path.resolve(here, "../../app/shell.svelte"), "utf8");
  const account = readFileSync(path.resolve(here, "account.svelte"), "utf8");
  const watch = readFileSync(path.resolve(here, "watch.svelte"), "utf8");
  expect(shell).toContain('href="#account"');
  expect(shell).toContain("routes/account/account.svelte");
  expect(shell).toContain('route === "#account"');
  expect(account).toContain("signInCode");
  expect(account).toContain("deletionCode");
  expect(account).toContain("Email a fresh code");
  expect(account).not.toContain("user_id");
  expect(watch).toContain("deletionLines");
  expect(watch).not.toContain("stream.current");
  expect(watch).not.toContain("stream.total");
  expect(watch).not.toContain("user_id");

  // Both disclosures are closed by default: the privacy table and the
  // erasure instructions open only when asked. The SSR render check in the
  // privacy test above still sees every sentence, because a details
  // carries its children in the HTML either way.
  expect(account).not.toContain("<details open");
  expect(account).toContain('class="disclosure privacy-drawer"');
  expect(account).toContain('class="disclosure erase-drawer"');
  // The nothing-automatic sentence lives inside the erasure disclosure, so
  // the Deletion section itself leads with the one stay sentence.
  const erase = account.indexOf('<details class="disclosure erase-drawer">');
  expect(erase).toBeGreaterThan(0);
  expect(account.indexOf("{accounts.notAutomatic}"), "the nothing-automatic sentence sits in the disclosure").toBeGreaterThan(
    erase,
  );
  expect(account.indexOf("{accounts.notAutomatic}")).toBeGreaterThan(account.indexOf("{accounts.stays}"));
});
