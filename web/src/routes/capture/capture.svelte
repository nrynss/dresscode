<script lang="ts">
  import { fileBackend, prepareImage, sessionBackend, CameraSession } from "@nrynss/chaaya/capture";
  import type { ChunkUploader } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../app/error-notice.svelte";
  import type { ShownError } from "../../lib/api/types";
  import {
    finishUpload,
    registerLink,
    registerProduct,
    registerSelfie,
    resumeUpload,
    shownError,
    stillOptions,
    uploadCaptured,
    type NormalisedPhoto,
    type UserProduct,
  } from "./upload";

  const session = new CameraSession();

  let video = $state<HTMLVideoElement | null>(null);
  let lens = $state<"selfie" | "product" | "">("");
  let busy = $state(false);
  let problem = $state<ShownError | undefined>(undefined);
  let status = $state("");
  let selfie = $state<NormalisedPhoto | undefined>(undefined);
  let productName = $state("");
  let productKind = $state("");
  let link = $state("");
  let savedProducts = $state<UserProduct[]>([]);
  let pending = $state<ChunkUploader | undefined>(undefined);
  let resumedId = $state<string | undefined>(undefined);
  let current = $state<ChunkUploader | undefined>(undefined);
  let sent = $state(0);
  let total = $state(0);

  // Copy the uploader's counters into this screen so the bar can move.
  $effect(() => {
    const uploader = current ?? pending;
    if (!uploader) {
      sent = 0;
      total = 0;
      return;
    }
    const read = () => {
      sent = uploader.stored;
      total = uploader.capturedBytes;
    };
    read();
    const timer = setInterval(read, 100);
    return () => clearInterval(timer);
  });

  $effect(() => {
    const element = video;
    if (!element) return;
    session.bind(element);
    return () => session.destroy();
  });

  $effect(() => {
    let alive = true;
    void resumeUpload()
      .then((uploader) => {
        if (!alive || !uploader || uploader.state === "failed") return;
        pending = uploader;
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  });

  function productReady(): boolean {
    if (productName.trim() !== "" && productKind.trim() !== "") return true;
    problem = { code: "capture", message: "Name the product and what it is." };
    return false;
  }

  async function openLens(next: "selfie" | "product"): Promise<void> {
    if (next === "product" && !productReady()) return;
    const element = video;
    if (!element) return;
    lens = next;
    problem = undefined;
    status = "";
    if (session.phase === "live" || session.phase === "requesting") session.stop();
    await session.start({ facing: next === "selfie" ? "user" : "environment", video: element });
  }

  async function take(): Promise<void> {
    if (lens !== "selfie" && lens !== "product") return;
    if (lens === "product" && !productReady()) return;
    const kind = lens;
    try {
      const blob = await sessionBackend(session).capture();
      session.stop();
      await bring(blob, kind);
    } catch (error) {
      problem = shownError(error);
    }
  }

  async function onGallery(event: Event, kind: "selfie" | "product"): Promise<void> {
    const input = event.currentTarget;
    if (!(input instanceof HTMLInputElement)) return;
    const files = input.files;
    input.value = "";
    if (!files || files.length === 0) return;
    if (kind === "product" && !productReady()) return;
    try {
      const blob = await fileBackend(files).capture();
      await bring(blob, kind);
    } catch (error) {
      problem = shownError(error);
    }
  }

  async function bring(blob: Blob, kind: "selfie" | "product"): Promise<void> {
    problem = undefined;
    status = "Preparing the photo.";
    busy = true;
    try {
      const prepared = await prepareImage(blob, stillOptions);
      status = "Sending the photo.";
      const receipt = await uploadCaptured(prepared.blob, {
        contentType: prepared.mimeType,
        onUploader: (uploader) => {
          current = uploader;
        },
      });
      if (kind === "selfie") {
        selfie = await registerSelfie(receipt.id);
        status = "The selfie is in.";
      } else {
        const saved = await registerProduct(receipt.id, productName.trim(), productKind.trim());
        savedProducts = [...savedProducts, saved];
        status = "The product is in.";
      }
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      busy = false;
      current = undefined;
    }
  }

  async function addLink(): Promise<void> {
    const url = link.trim();
    if (url === "") {
      problem = { code: "capture", message: "Paste a product link." };
      return;
    }
    problem = undefined;
    status = "Reading the link.";
    busy = true;
    try {
      const saved = await registerLink(url);
      savedProducts = [...savedProducts, saved];
      link = "";
      status = "The product is in.";
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      busy = false;
    }
  }

  async function sendRest(): Promise<void> {
    const uploader = pending;
    if (!uploader) return;
    problem = undefined;
    status = "Sending the photo.";
    busy = true;
    current = uploader;
    try {
      const receipt = await finishUpload(uploader);
      resumedId = receipt.id;
      pending = undefined;
      status = "The photo finished sending.";
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      busy = false;
      current = undefined;
    }
  }

  async function useResumed(kind: "selfie" | "product"): Promise<void> {
    const uploadId = resumedId;
    if (!uploadId) return;
    if (kind === "product" && !productReady()) return;
    problem = undefined;
    busy = true;
    try {
      if (kind === "selfie") {
        selfie = await registerSelfie(uploadId);
        status = "The selfie is in.";
      } else {
        const saved = await registerProduct(uploadId, productName.trim(), productKind.trim());
        savedProducts = [...savedProducts, saved];
        status = "The product is in.";
      }
      resumedId = undefined;
    } catch (error) {
      problem = shownError(error);
    } finally {
      busy = false;
    }
  }
</script>

<a class="back" href="#/">Back</a>
<p class="lead">One selfie for the occasion, and any product you are considering.</p>

{#if problem}
  <ErrorNotice error={problem} />
{/if}
{#if status}
  <p class="status" role="status">{status}</p>
{/if}
{#if current && total > 0}
  <progress aria-label="Sending the photo" value={sent} max={total}></progress>
{/if}

{#if pending}
  <p class="hint">A photo stopped on the way.</p>
  <button type="button" disabled={busy} onclick={() => void sendRest()}>Send the rest</button>
{/if}
{#if resumedId}
  <div class="row">
    <button type="button" disabled={busy} onclick={() => void useResumed("selfie")}>Use it as the selfie</button>
    <button type="button" disabled={busy} onclick={() => void useResumed("product")}>Use it as a product</button>
  </div>
{/if}

<section>
  <h2>Your selfie</h2>
  <div class="row">
    <button type="button" class="primary" disabled={busy} onclick={() => void openLens("selfie")}>
      Open the front camera
    </button>
  </div>
  <video
    bind:this={video}
    class:live={session.phase === "live"}
    class:mirror={session.facing === "user"}
    playsinline
    muted
    autoplay
    aria-label="Camera preview"
  ></video>
  {#if session.phase === "requesting"}
    <p class="hint">Opening the camera.</p>
  {/if}
  {#if session.phase === "live"}
    <button type="button" class="primary" disabled={busy} onclick={() => void take()}>Take the photo</button>
  {/if}
  {#if session.phase === "denied" || session.phase === "unavailable" || session.phase === "failed"}
    <p class="hint">
      {session.reason === "" ? "The camera did not open. Use the phone camera below." : session.reason}
    </p>
  {/if}
  <label class="file">
    Choose a photo
    <input type="file" accept="image/*" disabled={busy} onchange={(event) => void onGallery(event, "selfie")} />
  </label>
  <p class="hint">If that camera does not open, use the phone camera.</p>
  <label class="file">
    Use the phone camera
    <input
      type="file"
      accept="image/*"
      capture="user"
      disabled={busy}
      onchange={(event) => void onGallery(event, "selfie")}
    />
  </label>
  {#if selfie}
    <p class="hint">The selfie is ready.</p>
  {/if}
</section>

<section>
  <h2>A product</h2>
  <label class="field">
    Name
    <input bind:value={productName} autocomplete="off" />
  </label>
  <label class="field">
    What it is
    <input bind:value={productKind} autocomplete="off" />
  </label>
  <div class="row">
    <button type="button" disabled={busy} onclick={() => void openLens("product")}>Open the camera</button>
  </div>
  <label class="file">
    Choose a product photo
    <input type="file" accept="image/*" disabled={busy} onchange={(event) => void onGallery(event, "product")} />
  </label>
  <form
    onsubmit={(event) => {
      event.preventDefault();
      void addLink();
    }}
  >
    <label class="field">
      Link
      <input bind:value={link} autocomplete="off" />
    </label>
    <button type="submit" disabled={busy}>Add the link</button>
  </form>
  {#if savedProducts.length > 0}
    <ul>
      {#each savedProducts as product, index (index)}
        <li>{product.name}</li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .back {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    color: var(--accent);
  }

  section {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-top: 1.5rem;
  }

  h2 {
    margin: 0;
    font-size: 1.35rem;
    font-weight: 600;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }

  .row > button {
    flex: 1 1 100%;
  }

  .field,
  .file {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  input {
    width: 100%;
    min-height: 2.75rem;
    padding: 0.45rem 0.7rem;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
  }

  video {
    width: 100%;
    height: 0;
    background: var(--sunken);
    border-radius: var(--radius-control);
  }

  video.live {
    height: auto;
    max-height: 70dvh;
  }

  video.mirror {
    transform: scaleX(-1);
  }

  progress {
    width: 100%;
    height: 0.7rem;
    margin-top: 0.75rem;
    accent-color: var(--accent);
  }

  .hint,
  .status {
    margin: 0;
    color: var(--dim);
  }

  ul {
    margin: 0;
    padding-left: 1.1rem;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
</style>
