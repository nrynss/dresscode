import type { PrepareImageOptions } from "@nrynss/chaaya/capture";
import { ChunkUploader, UploadFailure, type UploadReceipt, type UploadStore } from "@nrynss/chaaya/keel";
import { ApiError, api } from "../../lib/api/client";
import type { ShownError } from "../../lib/api/types";
import { keptOccasion } from "../occasion/menu";

/** One chunk, matching the recorded upload fixtures. */
export const chunkBytes = 65536;

/**
 * Resize before upload. 4096 is Keel's default long side.
 * 4 MiB is the cap on one image sent to Clef.
 */
export const stillOptions = {
  maxLongSide: 4096,
  maxBytes: 4 * 1024 * 1024,
  mime: "image/jpeg",
} as const satisfies PrepareImageOptions;

const uploadOwner = "ignored";

export interface SendOptions {
  contentType: string;
  store?: UploadStore;
  onUploader?: (uploader: ChunkUploader) => void;
}

export interface NormalisedPhoto {
  media_id: string;
  url: string;
  content_type: string;
  sha256: string;
}

export interface UserProduct {
  id: string;
  category: string;
  name: string;
  fidelity: string;
  origin: string;
  media_id?: string;
  url?: string;
}

/** The occasion a look starts on when the picker has not saved a choice this
 * session: a fresh-session judge who starts straight away gets the story's
 * occasion. A save in the picker hands its choice over (keptOccasion). */
export const storyOccasion = "family-function";

export interface LookStart {
  lookId: string;
  jobId: string;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

/** Start a look: the occasion, the registered selfie, and the products the
 * user is considering. The occasion is the picker's saved choice when there
 * is one; with none saved this session, the caller's occasion stands (the
 * capture screen sends the story's). The answer names the look and the job
 * that follows it. A body without both is not a started look. */
export async function startLook(occasion: string, selfieId: string, productIds: readonly string[]): Promise<LookStart> {
  const saved = keptOccasion();
  const body = asRecord(
    await api<unknown>("/api/looks", {
      method: "POST",
      body: JSON.stringify({
        occasion: saved === "" ? occasion : saved,
        selfie_id: selfieId,
        product_ids: [...productIds],
      }),
    }),
  );
  const lookId = typeof body?.look_id === "string" ? body.look_id : "";
  const jobId = typeof body?.job_id === "string" ? body.job_id : "";
  if (lookId === "" || jobId === "") throw new Error("The look could not be started.");
  return { lookId, jobId };
}

/** Chaaya's open body has no group field. Omitted means selfie, including a product photo. */
export async function sendImage(bytes: Uint8Array, options: SendOptions): Promise<UploadReceipt> {
  const uploader = new ChunkUploader({
    url: "/api/uploads",
    owner: uploadOwner,
    contentType: options.contentType,
    visibility: "private",
    chunkSize: chunkBytes,
    store: options.store,
  });
  options.onUploader?.(uploader);
  await uploader.start();
  if (uploader.state === "failed") throw uploadFailure(uploader);
  uploader.append(asBuffer(bytes));
  return finishUpload(uploader);
}

export async function uploadCaptured(
  blob: Blob,
  options: Omit<SendOptions, "contentType"> & { contentType?: string } = {},
): Promise<UploadReceipt> {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  const contentType = options.contentType ?? (blob.type.startsWith("image/") ? blob.type : "image/jpeg");
  return sendImage(bytes, {
    contentType,
    store: options.store,
    onUploader: options.onUploader,
  });
}

export function resumeUpload(store?: UploadStore): Promise<ChunkUploader | undefined> {
  return ChunkUploader.resume(store);
}

export async function finishUpload(uploader: ChunkUploader): Promise<UploadReceipt> {
  await uploader.finish();
  if (uploader.state !== "done" || uploader.receipt === undefined) throw uploadFailure(uploader);
  return uploader.receipt;
}

export function registerSelfie(uploadId: string): Promise<NormalisedPhoto> {
  return api<NormalisedPhoto>("/api/photos", {
    method: "POST",
    body: JSON.stringify({ upload_id: uploadId }),
  });
}

export function registerProduct(uploadId: string, name: string, category: string): Promise<UserProduct> {
  return api<UserProduct>("/api/products", {
    method: "POST",
    body: JSON.stringify({ upload_id: uploadId, name, category }),
  });
}

/** True when the product's name and its kind are both typed, outside the
 * whitespace a phone keyboard leaves behind. */
export function productFieldsReady(name: string, kind: string): boolean {
  return name.trim() !== "" && kind.trim() !== "";
}

/** A product photo that waits for its name: the photo itself — the bytes to
 * send, or the id of an upload already on the server — and the sentence
 * naming what is missing. */
export interface ProductHold {
  photo: Blob | string;
  ask: string;
}

export type ProductGateAnswer = { pass: true } | { pass: false; hold: ProductHold };

/** The gate around a product photo's name and kind, answered before the pick
 * is consumed. The first real user picked a product photo with the fields
 * still empty and lost it: the old gate read the FileList, reset the input,
 * and only then refused — the refusal landed after the pick was already
 * voided. This gate never voids a pick. With the fields typed the photo
 * passes; without them it is held whole — the exact object continues, it is
 * never re-read from the dead FileList — and the screen asks for the name at
 * the fields. Once the fields are typed, the held photo completes the
 * product without a second pick. */
export function productGate(photo: Blob | string, name: string, kind: string): ProductGateAnswer {
  if (productFieldsReady(name, kind)) return { pass: true };
  return { pass: false, hold: { photo, ask: "Name the product and what it is." } };
}

export function registerLink(url: string): Promise<UserProduct> {
  return api<UserProduct>("/api/products/link", {
    method: "POST",
    body: JSON.stringify({ url }),
  });
}

/** A wire failure, or a local capture failure the screen can say. */
export function shownError(error: unknown): ShownError {
  if (error instanceof ApiError) {
    return { code: error.code, message: error.message, detail: error.detail, status: error.status };
  }
  if (error instanceof UploadFailure) {
    return { code: error.code, message: error.message, detail: error.detail };
  }
  if (error instanceof Error && error.message.trim() !== "") {
    return { code: "capture", message: error.message };
  }
  return { code: "network" };
}

function uploadFailure(uploader: ChunkUploader): UploadFailure {
  const error = uploader.error;
  if (!error) return new UploadFailure("network", "The upload stopped.", { retryable: true });
  return new UploadFailure(error.code, error.message, {
    detail: error.detail,
    retryable: error.retryable,
  });
}

function asBuffer(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy;
}
