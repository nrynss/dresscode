/** The screen's sentences and the readers that turn recorded bodies into
 * them. Every word of memory arrives from the API: the notice, the recall
 * sentence, and the looks in words. The screen's own copy names only the
 * switch and the sections, so a fresh screen says nothing about memory,
 * and no digit stands anywhere in a sentence. */

export const openingSentence = "Opening style history.";

export const openingVisitSentence = "Opening past looks.";

export const emptyVisitSentence = "There are no past looks yet.";

export const stillBeingFinishedSentence = "This look is still being finished.";

export const nothingYetSentence = "There is nothing to show for this look yet.";

/** The verdicts a past look can carry. They are words, never a figure. */
export const lookVerdicts = ["kept", "dismissed", "bought"] as const;

export type LookVerdict = (typeof lookVerdicts)[number];

export function isLookVerdict(value: string): value is LookVerdict {
  return (lookVerdicts as readonly string[]).includes(value);
}

/** The switch and the sentence of what is kept and where. */
export interface HistoryState {
  enabled: boolean;
  notice: string;
}

/** Recall for one occasion. The fix ids ride along for the picker; the
 * screen never shows them. */
export interface Recall {
  enabled: boolean;
  occasion: string;
  line: string;
  kept: string[];
  dismissed: string[];
}

/** One past look, as the list records it. The id and the fix id are keys,
 * never shown. */
export interface PastLook {
  id: string;
  verdict: LookVerdict | "";
  ready: boolean;
  words: string;
}

/** The line a past look shows: its words, or a plain sentence when the
 * check has not written any yet. */
export function lookLine(look: PastLook): string {
  if (!look.ready) return stillBeingFinishedSentence;
  return look.words === "" ? nothingYetSentence : look.words;
}

/** One occasion's group: its name, the sentence memory keeps for it, and
 * its past looks. */
export interface VisitGroup {
  occasion: { id: string; name: string };
  line: string;
  looks: PastLook[];
}

export interface SecondVisit {
  groups: VisitGroup[];
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/** The switch and its sentence, as the GET and the PUT record them. */
export function historyFromResponse(body: unknown): HistoryState {
  const record = asRecord(body);
  const enabled = record?.enabled;
  const notice = asString(record?.notice);
  if (typeof enabled !== "boolean" || notice === undefined) {
    throw new Error("The style history switch could not be read.");
  }
  return { enabled, notice };
}

function idsOf(value: unknown, what: string): string[] {
  if (!Array.isArray(value)) throw new Error(what);
  for (const item of value) {
    if (typeof item !== "string" || item === "") throw new Error(what);
  }
  return [...(value as string[])];
}

const recallError = "The memory of this occasion could not be read.";

/** Recall for one occasion. With history off the body is `{"enabled":false}`
 * and nothing else; a word of memory beside a closed switch is refused
 * here, so it cannot reach the screen, and another occasion's memory is
 * not this answer. */
export function recallFromResponse(body: unknown, occasion: string): Recall {
  const record = asRecord(body);
  if (record === undefined || typeof record.enabled !== "boolean") throw new Error(recallError);
  if (!record.enabled) {
    for (const member of ["line", "kept", "dismissed"] as const) {
      if (record[member] !== undefined) throw new Error(recallError);
    }
    return { enabled: false, occasion, line: "", kept: [], dismissed: [] };
  }
  const named = asString(record.occasion);
  const line = typeof record.line === "string" ? record.line : undefined;
  if (named !== occasion || line === undefined) throw new Error(recallError);
  return {
    enabled: true,
    occasion,
    line,
    kept: idsOf(record.kept, recallError),
    dismissed: idsOf(record.dismissed, recallError),
  };
}

const looksError = "The past looks could not be read.";

/** The past looks of one occasion, in the order the list returns. A look
 * of another occasion is not this list's answer. */
export function looksFromResponse(body: unknown, occasion: string): PastLook[] {
  const record = asRecord(body);
  const list = record?.looks;
  if (!record || !Array.isArray(list)) throw new Error(looksError);
  const looks: PastLook[] = [];
  for (const item of list) {
    const look = asRecord(item);
    if (look === undefined) throw new Error(looksError);
    const id = asString(look.id);
    const verdict = look.verdict;
    if (
      id === undefined ||
      look.occasion !== occasion ||
      typeof look.ready !== "boolean" ||
      typeof look.words !== "string" ||
      (verdict !== "" && (typeof verdict !== "string" || !isLookVerdict(verdict)))
    ) {
      throw new Error(looksError);
    }
    looks.push({ id, verdict: verdict === "" ? "" : verdict, ready: look.ready, words: look.words });
  }
  return looks;
}

/** The occasions, in the order the API returns, for grouping by occasion. */
export function occasionsOf(body: unknown): { id: string; name: string }[] {
  const list = asRecord(body)?.occasions;
  if (!Array.isArray(list)) throw new Error(looksError);
  const occasions: { id: string; name: string }[] = [];
  for (const item of list) {
    const occasion = asRecord(item);
    const id = occasion === undefined ? undefined : asString(occasion.id);
    const name = occasion === undefined ? undefined : asString(occasion.name);
    if (id === undefined || name === undefined) throw new Error(looksError);
    occasions.push({ id, name });
  }
  return occasions;
}
