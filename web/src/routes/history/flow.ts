import { ApiError, api } from "../../lib/api/client";
import type { ShownError } from "../../lib/api/types";
import {
  occasionsOf,
  historyFromResponse,
  looksFromResponse,
  recallFromResponse,
  type HistoryState,
  type PastLook,
  type Recall,
  type SecondVisit,
  type VisitGroup,
} from "./copy";

/** The switch as this page session stored it. History is off until a PUT,
 * and the one recorded read is the fresh first visit, so a session that
 * has turned the switch on keeps the value it sent until a full reload
 * asks again. The same accepted rule as the opt-in menu. */
let stored: boolean | undefined;

/** The sentence the API gave for each state, kept so the notice matches
 * the stored state even while the one recorded write covers only the
 * other. */
const notices = new Map<boolean, string>();

export function resetHistory(): void {
  stored = undefined;
  notices.clear();
}

function noteNotice(state: HistoryState): void {
  notices.set(state.enabled, state.notice);
}

function history(signal?: AbortSignal): Promise<unknown> {
  return api<unknown>("/api/history", signal ? { signal } : {});
}

/** The switch as it is stored. The screen reads this on entry. */
export async function readHistory(signal?: AbortSignal): Promise<HistoryState> {
  if (stored !== undefined) return { enabled: stored, notice: notices.get(stored) ?? "" };
  const state = historyFromResponse(await history(signal));
  noteNotice(state);
  return state;
}

/** Turn the switch on or off. A well-formed response stores the value sent. */
export async function writeHistory(enabled: boolean, signal?: AbortSignal): Promise<HistoryState> {
  const state = historyFromResponse(
    await api<unknown>("/api/history", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
      ...(signal ? { signal } : {}),
    }),
  );
  noteNotice(state);
  stored = enabled;
  return { enabled, notice: notices.get(enabled) ?? state.notice };
}

function occasionPath(id: string, path: string): string {
  return `${path}?occasion=${encodeURIComponent(id)}`;
}

async function readLooks(occasion: string, signal?: AbortSignal): Promise<PastLook[]> {
  return looksFromResponse(
    await api<unknown>(occasionPath(occasion, "/api/looks"), signal ? { signal } : {}),
    occasion,
  );
}

async function readRecall(occasion: string, signal?: AbortSignal): Promise<Recall> {
  return recallFromResponse(
    await api<unknown>(occasionPath(occasion, "/api/history/recall"), signal ? { signal } : {}),
    occasion,
  );
}

async function listOccasions(signal?: AbortSignal): Promise<{ id: string; name: string }[]> {
  return occasionsOf(await api<unknown>("/api/occasions", signal ? { signal } : {}));
}

/** An occasion whose list or sentence is not recorded here has no group
 * and no line; any other failure surfaces. */
async function quiet<T>(task: Promise<T>): Promise<T | undefined> {
  try {
    return await task;
  } catch (error: unknown) {
    if (error instanceof ApiError && error.code === "not_found") return undefined;
    throw error;
  }
}

/** The second visit: past looks by occasion, and the sentence memory keeps
 * for each. The looks are the user's own record and load whatever the
 * switch says — viewing a look that is already tried costs nothing. The
 * sentence is memory's half, and memory stays opt-in: the recall read
 * answers enabled false while the switch is off, and the group's line
 * stays empty. */
export async function loadSecondVisit(signal?: AbortSignal): Promise<SecondVisit> {
  const occasions = await listOccasions(signal);
  const groups: VisitGroup[] = [];
  for (const occasion of occasions) {
    const looks = await quiet(readLooks(occasion.id, signal));
    if (looks === undefined) continue;
    let line = "";
    if (looks.length > 0 && stored === true) {
      // The sentence is memory's half, and memory stays opt-in: with the
      // switch off in this session, not one recall request is made.
      const recall = await quiet(readRecall(occasion.id, signal));
      if (recall?.enabled === true) line = recall.line;
    }
    groups.push({ occasion, line, looks });
  }
  return { groups };
}

/** A sentence the screen can show. The wire phrase is not passed through. */
export function shownFailure(error: unknown): ShownError {
  if (error instanceof ApiError) return { code: error.code };
  const message = error instanceof Error && error.message !== "" ? error.message : "Style history could not be read.";
  return { code: "history", message };
}
