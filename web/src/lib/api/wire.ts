import { parseErrorEnvelope, parseJobEvent, type ErrorBody, type JobEvent } from "@nrynss/chaaya/keel";

export type { ErrorBody, JobEvent };

/** Read a failed JSON body. Anything that is not the wire envelope is undefined. */
export function readWireError(text: string): ErrorBody | undefined {
  const parsed = parseErrorEnvelope(text);
  if (!parsed.ok) return undefined;
  return parsed.value.error;
}

/** The job state carries `error` as the envelope's error member, not wrapped again. */
export function readJobStateError(state: unknown): ErrorBody | undefined {
  if (typeof state !== "object" || state === null || !("error" in state)) return undefined;
  return readWireError(JSON.stringify({ error: (state as { error: unknown }).error }));
}

/** Parse one recorded event stream into typed frames. A frame that does not parse is dropped. */
export function readJobEvents(text: string): JobEvent[] {
  const events: JobEvent[] = [];
  for (const piece of text.split("\n\n")) {
    if (piece.trim() === "") continue;
    const parsed = parseJobEvent(`${piece}\n\n`);
    if (!parsed.ok) continue;
    events.push(parsed.value);
  }
  return events;
}

/** The error frame wraps the wire envelope a second time. The state read does not. */
export function eventError(event: JobEvent): ErrorBody | undefined {
  if (event.name !== "error") return undefined;
  return event.error.error;
}
