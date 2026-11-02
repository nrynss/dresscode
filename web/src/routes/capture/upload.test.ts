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
import { PhotoPreview } from "./preview.svelte";
import {
  chunkBytes,
  finishUpload,
  productFieldsReady,
  productGate,
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

test("a product photo picked before its name and kind is held whole, never dropped", () => {
  const blob = new Blob([block()], { type: "image/jpeg" });

  // The fields are empty: the gate holds, and the hold carries the exact
  // photo the user chose — the pick is never voided the way the reset
  // FileList once voided it.
  const unnamed = productGate(blob, "", "");
  expect(unnamed.pass).toBe(false);
  if (unnamed.pass) throw new Error("the gate passed with the fields empty");
  expect(unnamed.hold.photo).toBe(blob);
  expect(unnamed.hold.ask).toBe("Name the product and what it is.");

  // Whitespace is not a name and it is not a kind.
  expect(productGate(blob, "  ", "earrings").pass).toBe(false);
  expect(productGate(blob, "Gold jhumkas", "   ").pass).toBe(false);

  // The fields typed, the photo passes and the product completes from it.
  expect(productGate(blob, " Gold jhumkas ", " earrings ").pass).toBe(true);

  // An upload already on the server waits by the same gate: the hold is
  // the id, so the registration needs no second tap and no re-send.
  const heldId = productGate("5e1f1e0000000000000000000000000a", "", "");
  expect(heldId.pass).toBe(false);
  if (!heldId.pass) expect(heldId.hold.photo).toBe("5e1f1e0000000000000000000000000a");

  // Readiness is the gate's other half: the screen's effect answers it to
  // release the hold.
  expect(productFieldsReady("Gold jhumkas", "earrings")).toBe(true);
  expect(productFieldsReady("Gold jhumkas", "")).toBe(false);
  expect(productFieldsReady("", "earrings")).toBe(false);
  expect(productFieldsReady(" Gold jhumkas ", " earrings ")).toBe(true);
});

test("a product pick with the fields empty is held and asked for, at the fields, without a re-pick", () => {
  const source = readFileSync(path.join(here, "capture.svelte"), "utf8");

  // The FileList is live (T5.22): the copy-out stays ahead of the reset,
  // and the whole pick is handed to bring after it — nothing between the
  // reset and the hand-off voids the pick.
  const gallery = source.slice(
    source.indexOf("async function onGallery"),
    source.indexOf("async function bring"),
  );
  const copied = gallery.indexOf("const files = input.files ? [...input.files] : null;");
  const reset = gallery.indexOf('input.value = "";');
  const handed = gallery.indexOf("await bring(blob, kind);");
  expect(copied).toBeGreaterThanOrEqual(0);
  expect(reset).toBeGreaterThan(copied);
  expect(handed).toBeGreaterThan(reset);
  expect(gallery).toContain("a refusal now would");
  // The old gate lived here and landed after the reset; it is gone from
  // the picker.
  expect(gallery).not.toContain("productGate");
  expect(source).not.toContain("productReady()");

  // The gate answers inside bring: an unnamed product photo is held whole,
  // with the sentence naming what is missing, and nothing is sent.
  const bring = source.slice(
    source.indexOf("async function bring"),
    source.indexOf("async function addLink"),
  );
  expect(bring).toContain("const answer = productGate(blob, productName, productKind);");
  expect(bring).toContain("if (!answer.pass) {");
  expect(bring).toContain("heldProduct = answer.hold;");
  expect(bring.indexOf("productGate(blob")).toBeLessThan(bring.indexOf("Preparing the photo."));

  // The camera paths refuse nothing before the shot: the photo is taken
  // first and meets the same gate in bring, so no lens flow loses work the
  // user has already done.
  const lens = source.slice(
    source.indexOf("async function openLens"),
    source.indexOf("async function onGallery"),
  );
  const take = source.slice(
    source.indexOf("async function take()"),
    source.indexOf("async function onGallery"),
  );
  expect(lens).not.toContain("productGate");
  expect(lens).not.toContain("Name the product");
  expect(take).not.toContain("productGate");
  expect(take).not.toContain("Name the product");
  expect(take).toContain("await bring(blob, kind);");

  // The held photo completes without a second pick: the effect waits for
  // the fields and releases the hold — bytes go to bring, an upload id
  // goes straight to its registration.
  const release = source.slice(
    source.indexOf("const held = heldProduct;"),
    source.indexOf("async function openLens"),
  );
  expect(release).toContain("if (!productFieldsReady(productName, productKind)) return;");
  expect(release).toContain("heldProduct = undefined;");
  expect(release).toContain("void useHeldUpload(held.photo);");
  expect(release).toContain("void bring(held.photo, \"product\");");

  // The ask stands at the fields, before the Name field it names, and
  // reads the hold's own sentence.
  const drawer = source.slice(
    source.indexOf('<details class="product-drawer'),
    source.indexOf("</details>"),
  );
  const ask = drawer.indexOf("{#if heldProduct}");
  const name = drawer.indexOf("Name");
  expect(ask).toBeGreaterThanOrEqual(0);
  expect(name).toBeGreaterThan(ask);
  expect(drawer.slice(ask, drawer.indexOf("{/if}"))).toContain("{heldProduct.ask}");

  // The resumed upload waits by the same hold: no tap is wasted on a
  // refusal the user cannot act past.
  const resumed = source.slice(
    source.indexOf("async function useResumed"),
    source.indexOf("async function checkDressCode"),
  );
  expect(resumed).toContain("productFieldsReady(productName, productKind)");
  expect(resumed).toContain("heldProduct = answer.hold;");
});

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

test("the mirror preview hands back each object URL, on replacement and on release", () => {
  const original = {
    create: URL.createObjectURL,
    revoke: URL.revokeObjectURL,
  };
  const revoked: string[] = [];
  let counter = 0;
  (URL as unknown as { createObjectURL: (blob: Blob) => string }).createObjectURL = (blob: Blob) => {
    expect(blob).toBeInstanceOf(Blob);
    counter += 1;
    return `blob:test/${counter}`;
  };
  (URL as unknown as { revokeObjectURL: (url: string) => void }).revokeObjectURL = (url: string) => {
    revoked.push(url);
  };
  try {
    const preview = new PhotoPreview();
    expect(preview.url).toBe("");
    preview.replace(new Blob(["first"], { type: "image/jpeg" }));
    expect(preview.url).toBe("blob:test/1");
    // A new photo revokes the one it replaces: the old bytes are not pinned.
    preview.replace(new Blob(["second"], { type: "image/jpeg" }));
    expect(preview.url).toBe("blob:test/2");
    expect(revoked).toEqual(["blob:test/1"]);
    // Releasing hands back the current URL and nothing after it.
    preview.release();
    expect(preview.url).toBe("");
    expect(revoked).toEqual(["blob:test/1", "blob:test/2"]);
    // A second release has nothing to hand back.
    preview.release();
    expect(revoked).toEqual(["blob:test/1", "blob:test/2"]);
  } finally {
    (URL as unknown as { createObjectURL: unknown }).createObjectURL = original.create;
    (URL as unknown as { revokeObjectURL: unknown }).revokeObjectURL = original.revoke;
  }
});

test("the screen grabs a front-camera frame and keeps a gallery and a phone-camera fallback", () => {
  const source = readFileSync(path.join(here, "capture.svelte"), "utf8");
  const shell = readFileSync(path.join(here, "../../app/shell.svelte"), "utf8");
  expect(captureHash).toBe("#/capture");
  // The bar's link must bind the constant, not a literal: a bare-word pin
  // here survives a swap to `href="#/capture"` and guards nothing. The
  // mounted side of this — that the rendered tab's href is exactly the
  // constant's value — lives in tests/a11y/shell-nav.test.ts.
  expect(shell).toContain("href={captureHash}");
  // The capture screen is the `Mirror` tab of the fixed bottom tab bar; the
  // text link that used to say `Bring the look` is gone from the shell.
  expect(shell).toContain("Mirror");
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

  // The vanity mirror: an arched portrait frame the spacer carries, and the
  // authored camera failure whatever the session says. The idle prompt and
  // the ready hint are gone — the mirror's emblem and the selfie photo speak
  // for themselves — and the top bar's back button is the way back, so the
  // screen draws none of its own.
  const styles = source.slice(source.indexOf("<style>"));
  expect(source).not.toContain("The mirror opens here.");
  expect(source).not.toContain("The selfie is ready.");
  expect(source).not.toContain('<a class="back"');
  expect(source).not.toContain("session.reason");
  expect(source).toContain("The camera did not open. Use the phone camera below.");
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*border-radius: 999px 999px var\(--radius-panel\) var\(--radius-panel\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*border: 2px solid var\(--line\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*background: var\(--sunken\)/);
  expect(styles).toMatch(/\.mirror-frame\s*\{[^}]*max-width: min\(100%, calc\(68dvh \* 3 \/ 4\)\)/);
  expect(styles).toMatch(/\.mirror-frame::before\s*\{[^}]*padding-top: 133%/);

  // The inner rim: a quiet line just inside the frame, wearing the arch, so
  // the empty glass is not a flat rectangle. Decorative, on the excused soft
  // edge, and it never swallows a tap.
  expect(styles).toMatch(/\.mirror-frame::after\s*\{[^}]*content: ""/);
  expect(styles).toMatch(/\.mirror-frame::after\s*\{[^}]*position: absolute/);
  expect(styles).toMatch(/\.mirror-frame::after\s*\{[^}]*border: 1px solid var\(--line-soft\)/);
  expect(styles).toMatch(/\.mirror-frame::after\s*\{[^}]*border-radius: inherit/);
  expect(styles).toMatch(/\.mirror-frame::after\s*\{[^}]*pointer-events: none/);

  // Zero instructional bloat: no lead paragraph, no visible heading (the
  // section names itself for the reader who arrives by landmark), and no
  // camera hand-holding hint above the fallbacks.
  expect(source).not.toContain("One selfie for the occasion");
  expect(source).not.toContain("<h2");
  expect(source).toContain('<section aria-label="Your selfie">');
  expect(source).not.toContain("If that camera does not open");

  // The chosen selfie shows inside the arch: the preview rides the prepared
  // bytes behind an object URL, fills the frame, and stands down while the
  // camera is live. The frame's curve and overflow clip it to the mirror.
  const mirror = source.slice(
    source.indexOf('<div class="mirror-frame">'),
    source.indexOf('{#if session.phase === "requesting"}'),
  );
  expect(mirror).toContain('class="mirror-preview"');
  expect(mirror).toContain('{#if preview.url !== "" && session.phase !== "live"}');
  expect(mirror).toContain('src={preview.url}');
  expect(mirror).toContain('alt="Your selfie."');
  expect(styles).toMatch(/\.mirror-frame \.mirror-preview\s*\{[^}]*position: absolute/);
  expect(styles).toMatch(/\.mirror-frame \.mirror-preview\s*\{[^}]*inset: 0/);
  expect(styles).toMatch(/\.mirror-frame \.mirror-preview\s*\{[^}]*width: 100%/);
  expect(styles).toMatch(/\.mirror-frame \.mirror-preview\s*\{[^}]*height: 100%/);
  expect(styles).toMatch(/\.mirror-frame \.mirror-preview\s*\{[^}]*object-fit: cover/);

  // Both camera controls live inside the arch: the idle state centres the
  // mirror emblem and the front-camera CTA on the glass — the emblem above
  // the button, stroked in the quiet role — and the live state overlays the
  // shutter at the frame's bottom centre.
  expect(mirror).toContain('class="mirror-idle"');
  expect(mirror).toContain('class="mirror-mark"');
  expect(mirror.indexOf("mirror-mark")).toBeLessThan(mirror.indexOf("Open camera"));
  expect(mirror).toContain('stroke="var(--dim)"');
  expect(mirror).toContain('onclick={() => void openLens("selfie")}');
  expect(mirror).toContain('class="primary take-btn"');
  expect(mirror).toContain("Take photo");
  expect(mirror).toContain('onclick={() => void take()}');
  expect(styles).toMatch(/\.mirror-idle\s*\{[^}]*position: absolute/);
  expect(styles).toMatch(/\.mirror-idle\s*\{[^}]*align-items: center/);
  expect(styles).toMatch(/\.take-btn\s*\{[^}]*position: absolute/);
  expect(styles).toMatch(/\.take-btn\s*\{[^}]*bottom: /);
  expect(styles).toMatch(/\.take-btn\s*\{[^}]*left: 50%/);

  // The ready CTA sits below the mirror — the one main action, full width —
  // and the two fallbacks share one compact row with nothing between the
  // pills. Each picker names itself in three words or fewer and carries its
  // own small icon ahead of the text, so both hold one line at the phone
  // width.
  const below = source.slice(
    source.indexOf('{#if session.phase === "requesting"}'),
    source.indexOf('<details class="product-drawer'),
  );
  const check = below.indexOf("Check look");
  const gallery = below.indexOf("Choose photo");
  // The denial hint also says "Use the phone camera below.", so the picker
  // label is found from the gallery pill onward.
  const phone = below.indexOf("Phone camera", gallery);
  expect(check).toBeGreaterThanOrEqual(0);
  expect(gallery).toBeGreaterThan(check);
  expect(phone).toBeGreaterThan(gallery);
  const between = below.slice(gallery, phone);
  expect(between.match(/<\/label>/g)?.length).toBe(1);
  // No paragraph slips between the pills (the pill's own `<path` is not one).
  expect(/<p[\s>]/.test(between), "a paragraph between the picker pills").toBe(false);
  expect(styles).toMatch(/\.row > \.file\s*\{[^}]*flex: 1 1 0/);
  expect(styles).toMatch(/\.file\s*\{[^}]*gap: /);
  const checkButton = below.slice(below.indexOf("{#if selfie}"), below.indexOf('<div class="row">'));
  expect(checkButton).toContain('class="primary"');
  expect(checkButton).toContain("Check look");
  // The theme's own primary recipe is what makes the CTA span the column.
  const theme = readFileSync(path.join(here, "../../app/theme.css"), "utf8");
  expect(theme).toMatch(/button\.primary,\s*\.btn-primary,\s*a\.primary-link\s*\{[^}]*width: 100%/);
  // Each picker pill carries its own small icon ahead of its label, stroked
  // in the accent the pill sits on.
  const pills = below.split('<label class="file">').slice(1);
  expect(pills, "the two selfie picker pills").toHaveLength(2);
  expect(pills[0]).toContain("Choose photo");
  expect(pills[0]).toContain("<svg");
  expect(pills[0]).toContain('stroke="var(--accent)"');
  expect(pills[1]).toContain("Phone camera");
  expect(pills[1]).toContain("<svg");
  expect(pills[1]).toContain('stroke="var(--accent)"');

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

  // The product drawer folds away in the shared accordion card style, with
  // its own summary as its only title, and the selfie's CTA starts the look.
  expect(source).toContain('<details class="product-drawer disclosure">');
  expect(source).toContain("Try your own piece");
  expect(source).not.toContain("A product");
  expect(source).toContain("startLook(");
  expect(source).toContain("storyOccasion");
  expect(source).toContain("verdictHash");
});
