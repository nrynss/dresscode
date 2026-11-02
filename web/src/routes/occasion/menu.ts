import { api } from "../../lib/api/client";
import { recallFromResponse } from "../history/copy";
import type { OccasionDetail, OccasionFix, OccasionSummary } from "./types";

/** Stored sets for this page session, keyed by occasion id.
 * A successful write keeps the set it sent: a 200 replaced that occasion's
 * ticks with that set. The recorded read is the empty first visit, and the
 * recorded write is one example, so neither is applied over a choice this
 * session already saved. A full reload asks again. */
const remembered = new Map<string, string[]>();

/** The occasion the picker is working on this session: recorded the moment it
 * is chosen, and re-recorded by a write of its fixes. The next look starts on
 * it. A write that fails keeps what was chosen. It is mirrored into the tab's
 * sessionStorage, so a reload on the mirror, or a return from the phone's own
 * camera app, still knows the occasion instead of losing it. */
let kept = "";

const keptKey = "dresscode.occasion";

function readStored(): string {
  try {
    return typeof sessionStorage === "undefined" ? "" : (sessionStorage.getItem(keptKey) ?? "");
  } catch {
    return "";
  }
}

function writeStored(id: string): void {
  try {
    if (typeof sessionStorage === "undefined") return;
    if (id === "") sessionStorage.removeItem(keptKey);
    else sessionStorage.setItem(keptKey, id);
  } catch {
    // Storage blocked: the in-memory choice still stands for this page.
  }
}

/** The occasion the picker set active, or "" before any choice this session. */
export function keptOccasion(): string {
  if (kept === "") kept = readStored();
  return kept;
}

/** Record the occasion the picker has in hand, the moment it is chosen. */
export function keepOccasion(id: string): void {
  kept = id;
  writeStored(id);
}

export function resetOccasionMenu(): void {
  remembered.clear();
  kept = "";
  writeStored("");
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function copyIds(value: readonly unknown[]): string[] {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const item of value) {
    if (typeof item !== "string" || item === "") throw new Error("the saved fixes could not be read");
    if (seen.has(item)) continue;
    seen.add(item);
    ids.push(item);
  }
  return ids;
}

function occasionsOf(body: unknown): OccasionSummary[] {
  const list = asRecord(body)?.occasions;
  if (!Array.isArray(list)) throw new Error("the occasion list could not be read");
  return list.map((item) => {
    const occasion = asRecord(item);
    if (!occasion || typeof occasion.id !== "string" || typeof occasion.name !== "string") {
      throw new Error("the occasion list could not be read");
    }
    return { id: occasion.id, name: occasion.name };
  });
}

function criteriaOf(value: unknown): OccasionDetail["criteria"] {
  if (!Array.isArray(value)) return [];
  const criteria: OccasionDetail["criteria"] = [];
  for (const item of value) {
    const criterion = asRecord(item);
    if (!criterion || typeof criterion.id !== "string" || typeof criterion.kind !== "string") continue;
    if (typeof criterion.suggestion !== "string") continue;
    criteria.push({ id: criterion.id, kind: criterion.kind, suggestion: criterion.suggestion });
  }
  return criteria;
}

function fixesOf(value: unknown): OccasionFix[] {
  if (!Array.isArray(value)) throw new Error("the occasion menu could not be read");
  return value.map((item) => {
    const fix = asRecord(item);
    if (!fix || typeof fix.id !== "string" || fix.id === "" || typeof fix.changes !== "string") {
      throw new Error("the occasion menu could not be read");
    }
    const criteria = Array.isArray(fix.criteria) ? copyIds(fix.criteria.filter((id) => typeof id === "string")) : [];
    return { id: fix.id, changes: fix.changes, criteria };
  });
}

function detailOf(body: unknown, id: string): OccasionDetail {
  const record = asRecord(body);
  if (!record || record.id !== id || typeof record.name !== "string") {
    throw new Error("the occasion menu could not be read");
  }
  return {
    id,
    name: record.name,
    criteria: criteriaOf(record.criteria),
    fixes: fixesOf(record.fixes),
  };
}

function ticksOf(body: unknown, id: string): string[] {
  const record = asRecord(body);
  if (!record || record.occasion !== id || !Array.isArray(record.ticked)) {
    throw new Error("the saved fixes could not be read");
  }
  return copyIds(record.ticked);
}

function optInPath(id: string): string {
  return `/api/occasions/${encodeURIComponent(id)}/opt-in`;
}

async function getJson(path: string, signal?: AbortSignal): Promise<unknown> {
  return api<unknown>(path, signal ? { signal } : {});
}

async function putJson(path: string, body: unknown, signal?: AbortSignal): Promise<unknown> {
  return api<unknown>(path, {
    method: "PUT",
    body: JSON.stringify(body),
    ...(signal ? { signal } : {}),
  });
}

/** The compact pill label per fix. Keyed by the fix's whole id where the
 * occasion changes the wording, and by the id's tail where every occasion
 * shares it. The words of a kept fix ride along by name only in this module,
 * never in a screen's own markup. */
const chipLabels: Readonly<Record<string, string>> = {
  "date-nude-lip": "Even nude lip",
  "interview-nude-lip": "Quiet nude lip",
  "funeral-nude-lip": "Quiet nude lip",
  "date-open-collar": "Open-collar shirt",
  "interview-buttoned-shirt": "Buttoned shirt",
  "funeral-dark-shirt": "Dark buttoned shirt",
  "interview-short-hair": "Short, neat hair",
  "funeral-short-beard": "Short, close beard",
  "festival-full-beard": "Full, even beard",
  "gold-jhumkas": "Gold jhumkas",
  "gold-chain": "Gold chain",
  "red-lip": "Red lip",
  "festive-top": "Festive mandarin top",
  "tie-hair": "Hair tied back",
  "short-beard": "Short, even beard",
  "closed-smile": "Calm smile",
};

/** The compact pill label for one fix: the exact id where the occasion names
 * the wording, else the id's tail, else the recorded sentence. `changes`
 * stays the sentence on the wire either way. */
export function fixChipLabel(fix: OccasionFix): string {
  const exact = chipLabels[fix.id];
  if (exact !== undefined) return exact;
  const tail = fix.id.slice(fix.id.indexOf("-") + 1);
  return chipLabels[tail] ?? fix.changes;
}

/** The five occasions, in the order the API returns. */
export async function listOccasions(signal?: AbortSignal): Promise<OccasionSummary[]> {
  return occasionsOf(await getJson("/api/occasions", signal));
}

/** The fix menu for one occasion. */
export async function readOccasion(id: string, signal?: AbortSignal): Promise<OccasionDetail> {
  return detailOf(await getJson(`/api/occasions/${encodeURIComponent(id)}`, signal), id);
}

/** Ticks for one occasion. A set stored earlier in this session wins. */
export async function readOptIn(id: string, signal?: AbortSignal): Promise<string[]> {
  if (remembered.has(id)) return [...(remembered.get(id) ?? [])];
  return ticksOf(await getJson(optInPath(id), signal), id);
}

/** Replace the ticked fixes for one occasion. A well-formed 200 stores the
 * set sent, and that occasion becomes the one the next look starts on. */
export async function writeOptIn(id: string, ticked: readonly string[], signal?: AbortSignal): Promise<string[]> {
  const ids = copyIds(ticked);
  ticksOf(await putJson(optInPath(id), { ticked: ids }, signal), id);
  remembered.set(id, ids);
  kept = id;
  return [...ids];
}

/** The line the occasion's past visits keep, or "" when there is none. The
 * screen asks only while the style-history switch is on, and the answer is
 * the recorded body for that occasion: the closed switch answers nothing.
 * A visit the story has not met answers not_found, and the caller decides
 * what an unread line is worth. */
export async function readRecallLine(id: string, signal?: AbortSignal): Promise<string> {
  const path = `/api/history/recall?occasion=${encodeURIComponent(id)}`;
  return recallFromResponse(await getJson(path, signal), id).line;
}
