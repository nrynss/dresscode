import { ApiError, JobStream, type JobSnapshot, type JobStatus } from "@nrynss/chaaya/keel";
import { api } from "../../../lib/api/client";
import type { ShownError } from "../../../lib/api/types";
import { verdictFromLook, type VerdictCopy } from "./copy";

/** The recorded family-function look that is already rendering.
 * The screen follows this story. The ids are not shown. */
export const storyLookId = "100c0000000000000000000000000001";

export const storyJobId = "10b00000000000000000000000000001";

const statuses = new Set<JobStatus>(["queued", "running", "done", "error", "cancelled", "interrupted"]);

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

/** Map the snake_case state read onto Chaaya's snapshot.
 * `current` and `total` are left out so a fraction cannot reach the screen.
 * A look omits them. A deletion's count is not this screen. */
function jobSnapshot(body: unknown): JobSnapshot {
  const record = asRecord(body);
  if (!record) throw new Error("The job could not be read.");
  const jobId = record.job_id;
  const status = record.status;
  if (typeof jobId !== "string" || typeof status !== "string" || !statuses.has(status as JobStatus)) {
    throw new Error("The job could not be read.");
  }
  const snapshot: JobSnapshot = { jobId, status: status as JobStatus };
  if (typeof record.stage === "string" && record.stage !== "") snapshot.stage = record.stage;
  const error = asRecord(record.error);
  if (error && typeof error.code === "string" && typeof error.message === "string") {
    snapshot.error =
      error.detail === undefined
        ? { code: error.code, message: error.message }
        : { code: error.code, message: error.message, detail: error.detail };
  }
  return snapshot;
}

/** The replacement attempt, when the state read names one. The same id is not a switch. */
function replacementJob(body: unknown, current: string): string | undefined {
  const next = asRecord(body)?.next_id;
  if (typeof next !== "string" || next === "" || next === current) return undefined;
  return next;
}

export async function loadVerdict(lookId: string, signal?: AbortSignal): Promise<VerdictCopy> {
  return verdictFromLook(await api<unknown>(`/api/looks/${lookId}`, { signal }));
}

/** One job stream. Chaaya pauses it while the document is hidden.
 * The keel options do not expose that switch. Leaving it unset keeps the default on. */
function openStream(jobId: string, onState: (body: unknown) => void): JobStream {
  return new JobStream({
    url: `/api/jobs/${jobId}/events`,
    requestInit: { credentials: "include" },
    fetchState: async () => {
      const body = await api<unknown>(`/api/jobs/${jobId}`);
      const snapshot = jobSnapshot(body);
      onState(body);
      return snapshot;
    },
  });
}

export interface LookFollow {
  readonly lookId: string;
  readonly jobId: string;
  readonly stream: JobStream;
  close(): void;
}

/** Follow a look's job. A `next_id` opens that attempt and keeps the same look. */
export function followLook(
  lookId: string,
  jobId: string,
  onStream: (stream: JobStream, jobId: string) => void,
): LookFollow {
  const followed = new Set<string>();
  let latest: JobStream | undefined;
  let currentJob = jobId;
  const follow: LookFollow = {
    lookId,
    get jobId() {
      return currentJob;
    },
    get stream() {
      if (!latest) throw new Error("The job could not be read.");
      return latest;
    },
    close() {
      latest?.close();
    },
  };

  const open = (id: string): void => {
    if (followed.has(id)) return;
    followed.add(id);
    const opened = openStream(id, (body) => {
      const next = replacementJob(body, id);
      if (!next) return;
      open(next);
    });
    opened.attach(() => {});
    latest?.close();
    latest = opened;
    currentJob = id;
    onStream(opened, id);
  };

  open(jobId);
  return follow;
}

/** A sentence the screen can show. The wire phrase is not passed through. */
export function shownFailure(error: unknown): ShownError {
  if (error instanceof ApiError) return { code: error.code };
  return { code: "verdict", message: "The verdict could not be read." };
}
