/** Screen words for a look. Shop, and any other stage, has none. */
const stageWords: Record<string, string> = {
  judge: "Checking",
  pick: "Choosing a fix",
  measure: "Choosing a fix",
  render: "Rendering",
  check: "Checking the render",
};

/** The four stages a look walks, in the order the screen shows them. */
export const stageOrder: readonly string[] = [
  "Checking",
  "Choosing a fix",
  "Rendering",
  "Checking the render",
];

export const pausedSentence = "This look waits while Dresscode is in the background.";

export const openingSentence = "Opening this look.";

export const readingSentence = "Reading the verdict.";

/** The look that flags nothing is the good outcome: there is no fix to name,
 * so the screen says so instead of throwing. */
export const cleanSentence = "Nothing needs changing; the outfit already meets the dress code.";

/** The suggestion-only outcome: the judge flagged the look, but no fix was
 * open to try — the occasion's fix menu had nothing ticked — so no fix was
 * picked and no render exists. The screen names that cause in the product's
 * own voice, a suggestion and never a score or a blame, and the card beside
 * it hands over the one-tap path to the occasion's fix menu. */
export const suggestionOnlySentence = "No fix is open to try yet; the look stands on its suggestions.";

export function stageWord(stage: string | undefined): string {
  if (stage === undefined || stage === "") return "";
  return stageWords[stage] ?? "";
}

/** The lines the verdict screen shows. The look id is not one of them.
 * `fix` and `why` are undefined when the judge flagged nothing: the outfit
 * already meets the dress code, and the screen says so instead of a fix that
 * does not exist. They are also both undefined — with `suggestionOnly` true —
 * when the judge flagged the look but no fix was open to try: the screen
 * names that and points at the occasion's fix menu. */
export interface VerdictCopy {
  lookId: string;
  reasons: string[];
  fix: string | undefined;
  why: string | undefined;
  /** True when the look ends on its suggestions because no fix was open to
   * try, never when the judge flagged nothing. */
  suggestionOnly: boolean;
}

export function verdictLines(copy: VerdictCopy): string[] {
  const lines = [...copy.reasons];
  if (copy.suggestionOnly) lines.push(suggestionOnlySentence);
  if (copy.fix !== undefined) lines.push(copy.fix);
  if (copy.why !== undefined) lines.push(copy.why);
  return lines;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

/** Reasons, the suggested fix, and why. A pending look is not a verdict. A
 * look whose judge flagged nothing is a verdict: its fix key rides null (the
 * server marshals the absent pick that way) or is absent, and the screen
 * answers it with the clean sentence. Flags that ride without a fix are the
 * suggestion-only outcome, and a verdict too: the judge flagged the look but
 * the pick had nothing open to choose, so the screen names that and points
 * at the occasion's fix menu. The two no-fix shapes are told apart by the
 * flags, not by the wire's own suggestion_only member — that member is true
 * for the clean look as well (look-nofix.json), because it means no render
 * stood, on every path that ends without one. */
export function verdictFromLook(body: unknown): VerdictCopy {
  const look = asRecord(body);
  const flags = look?.flags;
  if (!look || look.ready !== true || typeof look.id !== "string" || !Array.isArray(flags)) {
    throw new Error("The verdict could not be read.");
  }
  const reasons: string[] = [];
  for (const item of flags) {
    const flag = asRecord(item);
    if (!flag || typeof flag.suggestion !== "string" || flag.suggestion === "") {
      throw new Error("The verdict could not be read.");
    }
    reasons.push(flag.suggestion);
  }
  if (look.fix === null || look.fix === undefined) {
    return { lookId: look.id, reasons, fix: undefined, why: undefined, suggestionOnly: reasons.length > 0 };
  }
  const fix = asRecord(look.fix);
  if (!fix || typeof fix.changes !== "string" || typeof fix.why !== "string") {
    throw new Error("The verdict could not be read.");
  }
  return { lookId: look.id, reasons, fix: fix.changes, why: fix.why, suggestionOnly: false };
}
