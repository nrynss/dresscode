import { JobStream, type JobEvent, type JobSnapshot, type JobStatus } from "@nrynss/chaaya/keel";
import { api, ApiError } from "../../lib/api/client";

/** Keel accepts an empty choice or `switch`. `switch` does not copy the guest's looks. */
export type SignInChoice = "" | "switch";

export type VerifyOutcome = { ok: true; switched: boolean } | { ok: false; conflict: true };

export interface Session {
  user_id: string;
  kind: string;
}

export interface DeletableLook {
  id: string;
  label: string;
  selfieId?: string;
}

interface JobBody {
  job_id?: unknown;
}

interface ListedLook {
  id?: unknown;
  words?: unknown;
}

interface LookDetail {
  selfie_id?: unknown;
}

interface RawJob {
  job_id?: unknown;
  status?: unknown;
  stage?: unknown;
  current?: unknown;
  total?: unknown;
  next_id?: unknown;
  error?: unknown;
}

const jobStatuses: readonly JobStatus[] = ["queued", "running", "done", "error", "cancelled", "interrupted"];

function isStatus(value: string): value is JobStatus {
  return (jobStatuses as readonly string[]).includes(value);
}

function isPublicId(id: string): boolean {
  return /^[0-9a-f]{32}$/.test(id);
}

function isAbort(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

function detailRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function pushLine(lines: string[], line: string): void {
  if (lines.at(-1) !== line) lines.push(line);
}

export function sessionLine(kind: string): string {
  if (kind === "owner") return "You are signed in.";
  return "You are here as a guest.";
}

export async function readSession(signal?: AbortSignal): Promise<Session> {
  return api<Session>("/api/session", { signal });
}

export async function requestCode(address: string, signal?: AbortSignal): Promise<void> {
  await api("/api/session/code", {
    method: "POST",
    body: JSON.stringify({ address }),
    signal,
  });
}

/** An empty choice upgrades this guest in place. A conflict leaves the code live for `switch`. */
export async function verifySignIn(
  address: string,
  code: string,
  choice: SignInChoice,
  signal?: AbortSignal,
): Promise<VerifyOutcome> {
  try {
    await api("/api/session/code/verify", {
      method: "POST",
      body: JSON.stringify({ address, code, choice }),
      signal,
    });
  } catch (error) {
    if (error instanceof ApiError && error.code === "guest_data_conflict" && choice === "") {
      return { ok: false, conflict: true };
    }
    throw error;
  }
  return { ok: true, switched: choice === "switch" };
}

async function startDeletion(path: string, body: string | undefined, signal?: AbortSignal): Promise<string> {
  const started = await api<JobBody>(path, { method: "POST", body, signal });
  if (typeof started.job_id !== "string" || started.job_id === "") {
    throw new ApiError("The deletion did not start.", "internal_error", 0);
  }
  return started.job_id;
}

export function deletePhoto(id: string, signal?: AbortSignal): Promise<string> {
  if (!isPublicId(id)) throw new ApiError("That request could not be used.", "invalid_request", 400);
  return startDeletion(`/api/photos/${id}/deletion`, undefined, signal);
}

export function deleteLook(id: string, signal?: AbortSignal): Promise<string> {
  if (!isPublicId(id)) throw new ApiError("That request could not be used.", "invalid_request", 400);
  return startDeletion(`/api/looks/${id}/deletion`, undefined, signal);
}

export function deleteHistory(signal?: AbortSignal): Promise<string> {
  return startDeletion("/api/history/deletion", undefined, signal);
}

export function deleteAccount(address: string, code: string, signal?: AbortSignal): Promise<string> {
  return startDeletion("/api/account/deletion", JSON.stringify({ address, code }), signal);
}

/** Looks the account screen can delete. A missing occasion list is an empty page, not an error. */
export async function listDeletableLooks(signal?: AbortSignal): Promise<DeletableLook[]> {
  const page = await api<{ occasions?: { id?: unknown; name?: unknown }[] }>("/api/occasions", { signal });
  const occasions = Array.isArray(page.occasions) ? page.occasions : [];
  const rows: DeletableLook[] = [];
  for (const occasion of occasions) {
    if (typeof occasion.id !== "string" || typeof occasion.name !== "string") continue;
    let listed: { looks?: ListedLook[] };
    try {
      listed = await api<{ looks?: ListedLook[] }>(`/api/looks?occasion=${encodeURIComponent(occasion.id)}`, {
        signal,
      });
    } catch (error) {
      if (error instanceof ApiError && error.code === "not_found") continue;
      throw error;
    }
    for (const look of listed.looks ?? []) {
      if (typeof look.id !== "string" || !isPublicId(look.id)) continue;
      let selfieId: string | undefined;
      try {
        const detail = await api<LookDetail>(`/api/looks/${look.id}`, { signal });
        if (typeof detail.selfie_id === "string" && isPublicId(detail.selfie_id)) selfieId = detail.selfie_id;
      } catch (error) {
        if (isAbort(error)) throw error;
        if (!(error instanceof ApiError && error.code === "not_found")) throw error;
      }
      const words = typeof look.words === "string" ? look.words.trim() : "";
      rows.push({
        id: look.id,
        label: words === "" ? occasion.name : `${occasion.name}: ${words}`,
        selfieId,
      });
    }
  }
  return rows;
}

/**
 * Screen sentences for one deletion stream.
 * Stage `delete` is the account. Stage `erase` is a photo, a look, or history.
 * A count is never a sentence.
 */
export function deletionLines(events: readonly JobEvent[]): string[] {
  const lines: string[] = [];
  let account = false;
  for (const event of events) {
    if (event.name === "heartbeat") continue;
    if (event.name === "progress") {
      if (event.stage === "delete") {
        account = true;
        const rowsDone = detailRecord(event.detail)?.rows_done === true;
        pushLine(
          lines,
          rowsDone ? "Every stored part is confirmed and your sign-in is removed." : "Deleting the account.",
        );
      } else if (event.stage === "erase") {
        pushLine(lines, "Deleting.");
      }
      continue;
    }
    if (event.name === "done") {
      pushLine(lines, account ? "Every part has confirmed." : "Every target has confirmed.");
    }
  }
  return lines;
}

/** Subscribe to the job, then read its state. A missing state leaves the stream to finish. */
export function openDeletionStream(jobId: string, onNext?: (id: string) => void): JobStream {
  return new JobStream({
    url: `/api/jobs/${jobId}/events`,
    requestInit: { credentials: "include" },
    fetchState: () => readJob(jobId, onNext),
  });
}

async function readJob(jobId: string, onNext?: (id: string) => void): Promise<JobSnapshot> {
  const state = await api<RawJob>(`/api/jobs/${jobId}`);
  if (typeof state.next_id === "string" && isPublicId(state.next_id) && state.next_id !== jobId) {
    onNext?.(state.next_id);
  }
  if (typeof state.status !== "string" || !isStatus(state.status)) {
    throw new ApiError("The deletion could not be read.", "internal_error", 0);
  }
  const snapshot: JobSnapshot = {
    jobId: typeof state.job_id === "string" ? state.job_id : jobId,
    status: state.status,
  };
  if (typeof state.stage === "string") snapshot.stage = state.stage;
  if (typeof state.current === "number") snapshot.current = state.current;
  if (typeof state.total === "number") snapshot.total = state.total;
  const error = detailRecord(state.error);
  if (error && typeof error.code === "string" && typeof error.message === "string") {
    snapshot.error = { code: error.code, message: error.message, detail: error.detail };
  }
  return snapshot;
}
