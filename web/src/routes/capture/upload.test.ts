import { fileBackend } from "@nrynss/chaaya/capture";
import type { ChunkUploader, SessionRecord, StoredChunk, UploadStore } from "@nrynss/chaaya/keel";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { humanMessage } from "../../lib/api/errors";
import { loadFixtures } from "../../lib/api/fixtures";
import { installMock, lastCredentials, pinAnswer } from "../../lib/api/mock";
import type { ApiFixture } from "../../lib/api/types";
import { resetOccasionMenu, writeOptIn } from "../occasion/menu";
import { captureHash } from "./route";
import {
  chunkBytes,
  finishUpload,
  registerLink,
  registerProduct,
  registerSelfie,
  resumeUpload,
  sendImage,
  shownError,
  startLook,
  storyOccasion,
  uploadCaptured,
} from "./upload";

const here = path.dirname(fileURLToPath(import.meta.url));

afterEach(() => {
  installMock()();
});

function fixture(file: string): ApiFixture {
  const found = loadFixtures().find((item) => item.file === file);
  if (!found) throw new Error(`missing fixture ${file}`);
  return found;
}

function block(): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(new ArrayBuffer(chunkBytes));
  bytes[0] = 1;
  return bytes;
}

function galleryFile(): File {
  return new File([block()], "gallery.jpg", { type: "image/jpeg" });
}

function memoryStore(): UploadStore {
  const sessions: SessionRecord[] = [];
  const chunks: StoredChunk[] = [];
  return {
    async putSession(record) {
      const index = sessions.findIndex((item) => item.id === record.id);
      if (index >= 0) sessions[index] = record;
      else sessions.push(record);
    },
    async latestSession() {
      let newest: SessionRecord | undefined;
      for (const record of sessions) {
        if (!newest || record.startedAt > newest.startedAt) newest = record;
      }
      return newest;
    },
    async deleteSession(id) {
      const index = sessions.findIndex((item) => item.id === id);
      if (index >= 0) sessions.splice(index, 1);
    },
    async putChunk(chunk) {
      const index = chunks.findIndex((item) => item.id === chunk.id && item.index === chunk.index);
      if (index >= 0) chunks[index] = chunk;
      else chunks.push(chunk);
    },
    async listChunks(id) {
      return chunks.filter((item) => item.id === id).sort((left, right) => left.index - right.index);
    },
    async deleteChunks(id) {
      for (let index = chunks.length - 1; index >= 0; index -= 1) {
        if (chunks[index]?.id === id) chunks.splice(index, 1);
      }
    },
  };
}

function installTransport(dropPuts: () => boolean): {
  calls: string[];
  bodies: Map<string, string>;
  dropped: () => number;
  accepted: () => number;
} {
  const calls: string[] = [];
  const bodies = new Map<string, string>();
  let dropped = 0;
  let accepted = 0;
  const base = globalThis.fetch.bind(globalThis);
  globalThis.fetch = async (input, init) => {
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const target = new URL(raw, "http://dresscode.local").pathname;
    if (method === "PUT" && target.includes("/chunks/") && dropPuts()) {
      dropped += 1;
      throw new TypeError("failed to fetch");
    }
    const response = await base(input, init);
    const call = `${method} ${target}`;
    if (method === "GET" && /^\/api\/uploads\/[^/]+$/.test(target) && accepted === 0) {
      const json = (await response.json()) as Record<string, unknown>;
      json.received = [];
      json.missing = [0];
      json.stored_bytes = 0;
      calls.push(call);
      return new Response(`${JSON.stringify(json)}\n`, {
        status: response.status,
        headers: new Headers(response.headers),
      });
    }
    if (method === "PUT" && target.includes("/chunks/")) accepted += 1;
    calls.push(call);
    if (typeof init?.body === "string") bodies.set(call, init.body);
    return response;
  };
  return { calls, bodies, dropped: () => dropped, accepted: () => accepted };
}

test("a gallery file uploads and the photo is registered", async () => {
  installMock();
  const transport = installTransport(() => false);
  const store = memoryStore();
  const begin = fixture("upload-begin.json").body as { id: string };
  const blob = await fileBackend([galleryFile()]).capture();
  expect(blob).toBeInstanceOf(File);
  expect(blob.size).toBe(chunkBytes);

  let watched: ChunkUploader | undefined;
  const receipt = await uploadCaptured(blob, {
    store,
    onUploader: (uploader) => {
      watched = uploader;
    },
  });
  const photo = await registerSelfie(receipt.id);

  expect(receipt).toMatchObject({
    id: begin.id,
    sha256: (fixture("upload-complete.json").body as { sha256: string }).sha256,
    sizeBytes: chunkBytes,
  });
  expect(watched?.state).toBe("done");
  expect(watched?.acknowledged).toBe(1);
  expect(watched?.stored).toBe(chunkBytes);
  expect(watched?.capturedBytes).toBe(chunkBytes);
  expect(photo).toEqual(fixture("photo.json").body);
  expect(lastCredentials).toBe("include");
  expect(JSON.parse(transport.bodies.get("POST /api/uploads") ?? "{}")).toEqual({
    owner: "ignored",
    content_type: "image/jpeg",
    visibility: "private",
    chunk_size: chunkBytes,
  });
  expect(JSON.parse(transport.bodies.get("POST /api/photos") ?? "{}")).toEqual({ upload_id: begin.id });
  expect(transport.calls).toEqual([
    "POST /api/uploads",
    `PUT /api/uploads/${begin.id}/chunks/0`,
    `GET /api/uploads/${begin.id}`,
    `POST /api/uploads/${begin.id}/complete`,
    "POST /api/photos",
  ]);
  expect(await store.latestSession()).toBeUndefined();
});

test("a dropped connection resumes and the upload completes", async () => {
  installMock();
  let connected = false;
  const transport = installTransport(() => !connected);
  const store = memoryStore();
  const begin = fixture("upload-begin.json").body as { id: string };

  await expect(sendImage(block(), { contentType: "image/jpeg", store })).rejects.toMatchObject({ code: "network" });
  expect(transport.dropped()).toBeGreaterThan(0);
  expect(transport.accepted()).toBe(0);
  expect(await store.latestSession()).toMatchObject({ id: begin.id });

  connected = true;
  const uploader = await resumeUpload(store);
  if (!uploader) throw new Error("nothing to resume");
  const receipt = await finishUpload(uploader);

  expect(uploader.state).toBe("done");
  expect(receipt.id).toBe(begin.id);
  expect(transport.accepted()).toBe(1);
  expect(transport.calls).toEqual([
    "POST /api/uploads",
    `GET /api/uploads/${begin.id}`,
    `PUT /api/uploads/${begin.id}/chunks/0`,
    `GET /api/uploads/${begin.id}`,
    `POST /api/uploads/${begin.id}/complete`,
  ]);
  expect(await store.latestSession()).toBeUndefined();
}, 20000);

test("a product photo and a product link register", async () => {
  installMock();
  const transport = installTransport(() => false);
  const store = memoryStore();
  const blob = await fileBackend([new File([block()], "jhumkas.jpg", { type: "image/jpeg" })]).capture();
  const receipt = await uploadCaptured(blob, { store });
  const photo = await registerProduct(receipt.id, "Gold jhumkas", "earrings");
  const linked = await registerLink("https://example.com/gold-jhumkas");

  expect(photo).toEqual(fixture("product-photo.json").body);
  expect(linked).toEqual(fixture("product-link.json").body);
  expect(JSON.parse(transport.bodies.get("POST /api/products") ?? "{}")).toEqual({
    upload_id: receipt.id,
    name: "Gold jhumkas",
    category: "earrings",
  });
  expect(JSON.parse(transport.bodies.get("POST /api/products/link") ?? "{}")).toEqual({
    url: "https://example.com/gold-jhumkas",
  });
});

test("no face, too small, and paused uploads use the client sentences", async () => {
  installMock();
  const cases: { file: string; method: string; path: string; run: () => Promise<unknown> }[] = [
    {
      file: "errors/uploads-paused.json",
      method: "POST",
      path: "/api/uploads",
      run: () => sendImage(block(), { contentType: "image/jpeg", store: memoryStore() }),
    },
    {
      file: "errors/no-face-in-photo.json",
      method: "POST",
      path: "/api/photos",
      run: () => registerSelfie("5e1f1e0000000000000000000000000a"),
    },
    {
      file: "errors/too-small.json",
      method: "POST",
      path: "/api/photos",
      run: () => registerSelfie("5e1f1e0000000000000000000000000a"),
    },
    {
      file: "errors/upload-a-photo.json",
      method: "POST",
      path: "/api/products/link",
      run: () => registerLink("https://example.com/gold-jhumkas"),
    },
  ];

  for (const item of cases) {
    const recorded = fixture(item.file);
    pinAnswer(item.method, item.path, recorded);
    const body = recorded.body as { error: { code: string; message: string } };
    const error = await item.run().then(
      () => undefined,
      (caught: unknown) => caught,
    );
    const shown = humanMessage(shownError(error));
    expect(shown, body.error.code).toBe(humanMessage({ code: body.error.code, message: body.error.message }));
    expect(shown, body.error.code).not.toBe(body.error.message);
    expect(shown.endsWith("."), body.error.code).toBe(true);
  }
});

test("starting the look posts the occasion, the selfie and the products", async () => {
  installMock();
  const inner = globalThis.fetch;
  const calls: { method: string; path: string; body: string }[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const url = new URL(raw, "http://dresscode.local");
    calls.push({
      method: (init?.method ?? "GET").toUpperCase(),
      path: url.pathname,
      body: typeof init?.body === "string" ? init.body : "",
    });
    return inner(input, init);
  }) as typeof fetch;

  const started = await startLook(storyOccasion, "5e1f1e00000000000000000000000001", [
    "b20d0c70000000000000000000000001",
  ]);
  const begin = fixture("look-start.json").body as { look_id: string; job_id: string };
  expect(started).toEqual({ lookId: begin.look_id, jobId: begin.job_id });
  expect(calls).toEqual([
    {
      method: "POST",
      path: "/api/looks",
      body: JSON.stringify({
        occasion: "family-function",
        selfie_id: "5e1f1e00000000000000000000000001",
        product_ids: ["b20d0c70000000000000000000000001"],
      }),
    },
  ]);

  // An answer without both ids is not a started look.
  pinAnswer("POST", "/api/looks", {
    file: "look-start-unreadable",
    method: "POST",
    path: "/api/looks",
    status: 202,
    body: { look_id: begin.look_id },
  });
  await expect(startLook("family-function", "5e1f1e00000000000000000000000001", [])).rejects.toThrow();
});

test("a saved picker occasion starts the look", async () => {
  installMock();
  resetOccasionMenu();
  pinAnswer("PUT", "/api/occasions/interview/opt-in", {
    file: "interview-opt-in-put",
    method: "PUT",
    path: "/api/occasions/interview/opt-in",
    status: 200,
    body: { occasion: "interview", ticked: ["interview-tie-hair"] },
  });
  const inner = globalThis.fetch;
  const bodies = new Map<string, string>();
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const target = new URL(raw, "http://dresscode.local").pathname;
    if (method === "POST" && target === "/api/looks" && typeof init?.body === "string") {
      bodies.set("POST /api/looks", init.body);
    }
    return inner(input, init);
  }) as typeof fetch;

  // Saving Interview hands the picker's choice to the next look start,
  // whatever occasion the screen passes as its fresh-session fallback.
  await writeOptIn("interview", ["interview-tie-hair"]);
  await startLook(storyOccasion, "5e1f1e00000000000000000000000001", []);
  expect(bodies.get("POST /api/looks")).toBe(
    JSON.stringify({
      occasion: "interview",
      selfie_id: "5e1f1e00000000000000000000000001",
      product_ids: [],
    }),
  );
});

test("the screen grabs a front-camera frame and keeps a gallery and a phone-camera fallback", () => {
  const source = readFileSync(path.join(here, "capture.svelte"), "utf8");
  const shell = readFileSync(path.join(here, "../../app/shell.svelte"), "utf8");
  expect(captureHash).toBe("#/capture");
  expect(shell).toContain("captureHash");
  expect(shell).toContain("Bring the look");
  expect(shell).toContain("isCaptureHash");
  expect(source).toContain("sessionBackend");
  expect(source).toContain("fileBackend");
  expect(source).toContain("prepareImage(blob, stillOptions)");
  expect(source).toContain('facing: next === "selfie" ? "user" : "environment"');
  expect(source).toContain('accept="image/*"');
  expect(source).toContain('capture="user"');
  expect(source.match(/type="file"/g)?.length).toBe(3);
  expect(source).toContain("ErrorNotice");
  expect(source).toContain('aria-label="Sending the photo"');

  // The vanity mirror: an arched portrait frame the spacer carries, with its
  // idle prompt, and the authored camera failure whatever the session says.
  const styles = source.slice(source.indexOf("<style>"));
  expect(source).toContain("The mirror opens here.");
  expect(source).not.toContain("session.reason");
  expect(source).toContain("The camera did not open. Use the phone camera below.");
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*border-radius: 999px 999px var\(--radius-panel\) var\(--radius-panel\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*border: 2px solid var\(--line\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*background: var\(--sunken\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*max-width: min\(100%, calc\(68dvh \* 3 \/ 4\)\)/);
  expect(styles).toMatch(/\.mirror-frame::before\s*\{[^}]*padding-top: 133%/);

  // The pickers wear the button, and the native input hides inside the label.
  expect(styles).toMatch(/\.file\s*\{[^}]*background: var\(--surface\)/);
  expect(styles).toMatch(/\.file\s*\{[^}]*border: 1px solid var\(--line\)/);
  expect(styles).toMatch(/\.file\s*\{[^}]*border-radius: var\(--radius-control\)/);
  expect(styles).toMatch(/\.file\s*\{[^}]*min-height: 2\.75rem/);
  expect(styles).toMatch(/\.file input\s*\{[^}]*position: absolute/);
  expect(styles).toMatch(/\.file input\s*\{[^}]*width: 1px/);
  expect(styles).toMatch(/\.file input\s*\{[^}]*height: 1px/);
  expect(styles).toMatch(/\.file input\s*\{[^}]*opacity: 0/);
  expect(styles).toMatch(/\.file input\s*\{[^}]*overflow: hidden/);

  // The product drawer folds away, and the selfie's CTA starts the look.
  expect(source).toContain('<details class="product-drawer">');
  expect(source).toContain("Considering a specific piece?");
  expect(source).toContain("Check the dress code");
  expect(source).toContain("startLook(");
  expect(source).toContain("storyOccasion");
  expect(source).toContain("verdictHash");
});
