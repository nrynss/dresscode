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
});
