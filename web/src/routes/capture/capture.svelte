<script lang="ts">
  import { fileBackend, prepareImage, sessionBackend, CameraSession } from "@nrynss/chaaya/capture";
  import type { ChunkUploader } from "@nrynss/chaaya/keel";
  import { onDestroy } from "svelte";
  import ErrorNotice from "../../app/error-notice.svelte";
  import { PhotoPreview } from "./preview.svelte";
  import type { ShownError } from "../../lib/api/types";
  import { verdictHash } from "../look/verdict/route";
  import {
    finishUpload,
    productFieldsReady,
    productGate,
    registerLink,
    registerProduct,
    registerSelfie,
    resumeUpload,
    shownError,
    startLook,
    stillOptions,
    storyOccasion,
    uploadCaptured,
    type NormalisedPhoto,
    type ProductHold,
    type UserProduct,
  } from "./upload";

  const session = new CameraSession();

  /** The mirror's selfie preview: the object URL over the photo's prepared
   * bytes, revoked on replacement and when the screen goes away. */
  const preview = new PhotoPreview();
  onDestroy(() => preview.release());

  let video = $state<HTMLVideoElement | null>(null);
  let lens = $state<"selfie" | "product" | "">("");
  let busy = $state(false);
  let starting = $state(false);
  let problem = $state<ShownError | undefined>(undefined);
  let status = $state("");
  let selfie = $state<NormalisedPhoto | undefined>(undefined);
  let productName = $state("");
  let productKind = $state("");
  /** A chosen product photo that waits for its name and kind. It stays
   * here whole — the pick is never voided — and completes the product the
   * moment the fields are typed, without a second pick. */
  let heldProduct = $state<ProductHold | undefined>(undefined);
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

  /** The gate's other half: a held photo leaves the moment the fields are
   * typed. The bytes go through the usual prepare-and-send path; an upload
   * already on the server goes straight to its registration. Either way the
   * product completes without a second pick. */
  $effect(() => {
    const held = heldProduct;
    if (held === undefined) return;
    if (!productFieldsReady(productName, productKind)) return;
    heldProduct = undefined;
    if (typeof held.photo === "string") void useHeldUpload(held.photo);
    else void bring(held.photo, "product");
  });

  async function openLens(next: "selfie" | "product"): Promise<void> {
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
    // The FileList is live: resetting the input empties it, so the chosen
    // files are copied out before the reset, or the reset swallows the pick.
    const files = input.files ? [...input.files] : null;
    input.value = "";
    if (!files || files.length === 0) return;
    // No gate here: the pick is already in hand, and a refusal now would
    // void it. The gate answers inside bring, where an unnamed product
    // photo is held instead of dropped.
    try {
      const blob = await fileBackend(files).capture();
      await bring(blob, kind);
    } catch (error) {
      problem = shownError(error);
    }
  }

  async function bring(blob: Blob, kind: "selfie" | "product"): Promise<void> {
    if (kind === "product") {
      const answer = productGate(blob, productName, productKind);
      if (!answer.pass) {
        // The photo waits whole for its name; the ask stands at the fields.
        heldProduct = answer.hold;
        return;
      }
    }
    problem = undefined;
    status = "Preparing the photo.";
    busy = true;
    try {
      const prepared = await prepareImage(blob, stillOptions);
      if (kind === "selfie") preview.replace(prepared.blob);
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
        await saveProduct(receipt.id);
      }
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      busy = false;
      current = undefined;
    }
  }

  /** Save the product from an upload already on the server, under the name
   * and kind as typed. */
  async function saveProduct(uploadId: string): Promise<void> {
    const saved = await registerProduct(uploadId, productName.trim(), productKind.trim());
    savedProducts = [...savedProducts, saved];
    status = "The product is in.";
  }

  /** The held upload id completing: the same registration the resumed
   * button would have run, with no second tap. */
  async function useHeldUpload(uploadId: string): Promise<void> {
    problem = undefined;
    busy = true;
    try {
      await saveProduct(uploadId);
      if (resumedId === uploadId) resumedId = undefined;
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      busy = false;
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
    if (kind === "product" && !productFieldsReady(productName, productKind)) {
      // The upload is safe on the server; the product waits for its name
      // by the same hold as a fresh pick, and completes the moment the
      // fields are typed — no second tap.
      const answer = productGate(uploadId, productName, productKind);
      if (!answer.pass) heldProduct = answer.hold;
      return;
    }
    problem = undefined;
    busy = true;
    try {
      if (kind === "selfie") {
        selfie = await registerSelfie(uploadId);
        status = "The selfie is in.";
      } else {
        await saveProduct(uploadId);
      }
      resumedId = undefined;
    } catch (error) {
      problem = shownError(error);
    } finally {
      busy = false;
    }
  }

  /** The one main action: the look starts, and the verdict screen follows it. */
  async function checkDressCode(): Promise<void> {
    const photo = selfie;
    if (!photo || starting) return;
    starting = true;
    problem = undefined;
    status = "Opening the look.";
    try {
      await startLook(
        storyOccasion,
        photo.media_id,
        savedProducts.map((product) => product.id),
      );
      status = "";
      if (typeof window !== "undefined") window.location.hash = verdictHash;
    } catch (error) {
      status = "";
      problem = shownError(error);
    } finally {
      starting = false;
    }
  }
</script>

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

<section aria-label="Your selfie">
  <div class="mirror-frame">
    <video
      bind:this={video}
      class:live={session.phase === "live"}
      class:mirror={session.facing === "user"}
      playsinline
      muted
      autoplay
      aria-label="Camera preview"
    ></video>
    {#if preview.url !== "" && session.phase !== "live"}
      <img class="mirror-preview" src={preview.url} alt="Your selfie." />
    {/if}
    {#if session.phase !== "live" && session.phase !== "requesting" && preview.url === ""}
      <div class="mirror-idle">
        <svg class="mirror-mark" viewBox="0 0 48 48" width="56" height="56" fill="none" aria-hidden="true">
          <path
            d="M15.5 20.5a8.5 8.5 0 0 1 17 0v12.75a2.75 2.75 0 0 1-2.75 2.75h-11.5a2.75 2.75 0 0 1-2.75-2.75V20.5Z"
            stroke="var(--dim)"
            stroke-width="1.5"
          />
          <path d="M19 18.5l4-4M18.75 23.25l6.5-6.5" stroke="var(--dim)" stroke-width="1.5" stroke-linecap="round" />
          <path
            d="M32.5 9.5l1.05 2.95 2.95 1.05-2.95 1.05-1.05 2.95-1.05-2.95-2.95-1.05 2.95-1.05 1.05-2.95Z"
            stroke="var(--dim)"
            stroke-width="1.5"
            stroke-linejoin="round"
          />
        </svg>
        <button type="button" class="primary" disabled={busy} onclick={() => void openLens("selfie")}>
          Open camera
        </button>
      </div>
    {/if}
    {#if session.phase === "live"}
      <button type="button" class="primary take-btn" disabled={busy} onclick={() => void take()}>
        Take photo
      </button>
    {/if}
  </div>
  {#if session.phase === "requesting"}
    <p class="hint">Opening the camera.</p>
  {/if}
  {#if session.phase === "denied" || session.phase === "unavailable" || session.phase === "failed"}
    <p class="hint">The camera did not open. Use the phone camera below.</p>
  {/if}
  {#if selfie}
    <button type="button" class="primary" disabled={busy || starting} onclick={() => void checkDressCode()}>
      Check look
    </button>
  {/if}
  <div class="row">
    <label class="file">
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" aria-hidden="true">
        <rect x="3.5" y="5.25" width="17" height="13.5" rx="2" stroke="var(--accent)" stroke-width="1.75" />
        <path
          d="M6.25 15.75l3.25-3.25 2.5 2.5 2.5-2.5 3.25 3.25"
          stroke="var(--accent)"
          stroke-width="1.75"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
        <circle cx="9.25" cy="9.5" r="1.3" stroke="var(--accent)" stroke-width="1.75" />
      </svg>
      Choose photo
      <input type="file" accept="image/*" disabled={busy} onchange={(event) => void onGallery(event, "selfie")} />
    </label>
    <label class="file">
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" aria-hidden="true">
        <path
          d="M3.75 9.75a1.9 1.9 0 0 1 1.9-1.9h1.8l1.15-1.7a1.9 1.9 0 0 1 1.6-.9h3.6a1.9 1.9 0 0 1 1.6.9l1.15 1.7h1.8a1.9 1.9 0 0 1 1.9 1.9v6.85a1.9 1.9 0 0 1-1.9 1.9H5.65a1.9 1.9 0 0 1-1.9-1.9V9.75Z"
          stroke="var(--accent)"
          stroke-width="1.75"
          stroke-linejoin="round"
        />
        <circle cx="12" cy="13" r="2.9" stroke="var(--accent)" stroke-width="1.75" />
      </svg>
      Phone camera
      <input
        type="file"
        accept="image/*"
        capture="user"
        disabled={busy}
        onchange={(event) => void onGallery(event, "selfie")}
      />
    </label>
  </div>
</section>

<details class="product-drawer disclosure">
  <summary>Try your own piece</summary>
  <section>
    {#if heldProduct}
      <!-- The ask stands at the fields, where the pick is waiting: the photo
       * is held whole above, and the product completes the moment both
       * fields are typed. -->
      <p class="hint" role="status">{heldProduct.ask}</p>
    {/if}
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
</details>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-top: 1.5rem;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }

  .row > button {
    flex: 1 1 100%;
  }

  /* The two selfie fallbacks share one compact row: each pill takes half of
   * it, so no picker sits on its own line. The tighter side padding is what
   * keeps a pill's icon and its label on one line at the phone width — the
   * theme's full side padding would push a 17px label to wrap. */
  .row > .file {
    flex: 1 1 0;
    min-inline-size: 0;
    padding-inline: 0.5rem;
  }

  .field {
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

  /* The arched vanity mirror. A portrait box the spacer carries the height of;
   * the arch rounds the two top corners fully. */
  .mirror-frame {
    position: relative;
    width: 100%;
    max-width: min(100%, calc(68dvh * 3 / 4));
    margin-inline: auto;
    border: 2px solid var(--line);
    border-radius: 999px 999px var(--radius-panel) var(--radius-panel);
    background: var(--sunken);
    overflow: hidden;
  }

  .mirror-frame::before {
    content: "";
    display: block;
    padding-top: 133%;
  }

  /* The inner rim: a quiet line just inside the frame, so the glass reads as
   * glass and the empty mirror is not a flat rectangle. Decorative, so it
   * rides the soft edge the contrast tables excuse. */
  .mirror-frame::after {
    content: "";
    position: absolute;
    inset: 0.6rem;
    border: 1px solid var(--line-soft);
    border-radius: inherit;
    pointer-events: none;
  }

  .mirror-frame video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  /* The chosen selfie, filling the arch. The frame's own curve and overflow
   * clip it, so the photo wears the mirror's shape. */
  .mirror-frame .mirror-preview {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .mirror-frame video.mirror {
    transform: scaleX(-1);
  }

  /* Inside the arch: the idle state centres the mirror emblem and the
   * front-camera CTA on the sunken glass; the live state overlays the shutter
   * at the bottom centre. */
  .mirror-idle {
    position: absolute;
    inset: 0;
    z-index: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1rem;
    padding: 1.5rem;
  }

  .take-btn {
    position: absolute;
    bottom: 1.25rem;
    left: 50%;
    translate: -50% 0;
    z-index: 1;
  }

  /* The file pickers wear the button, and the native input hides inside. The
   * gap keeps each pill's icon and its label on one line at the phone width. */
  .file {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    min-height: 2.75rem;
    padding: 0.45rem 0.9rem;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    cursor: pointer;
    overflow-wrap: break-word;
  }

  .file svg {
    flex: none;
  }

  .file:has(input:focus-visible) {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }

  .file input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    overflow: hidden;
  }

  .product-drawer {
    margin-top: 1.5rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: var(--surface);
    min-inline-size: 0;
  }

  .product-drawer summary {
    display: flex;
    align-items: center;
    min-height: 2.75rem;
    padding: 0.45rem 0.9rem;
    cursor: pointer;
    color: var(--text);
    overflow-wrap: break-word;
  }

  .product-drawer section {
    margin-top: 0;
    padding: 0 0.9rem 0.9rem;
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

  @media (prefers-reduced-motion: no-preference) {
    .file {
      transition:
        background-color 0.15s ease,
        border-color 0.15s ease;
    }
  }
</style>
