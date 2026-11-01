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

export function stageWord(stage: string | undefined): string {
  if (stage === undefined || stage === "") return "";
  return stageWords[stage] ?? "";
}

/** The lines the verdict screen shows. The look id is not one of them.
 * `fix` and `why` are undefined when the judge flagged nothing: the outfit
 * already meets the dress code, and the screen says so instead of a fix that
 * does not exist. */
export interface VerdictCopy {
  lookId: string;
  reasons: string[];
  fix: string | undefined;
  why: string | undefined;
}

export function verdictLines(copy: VerdictCopy): string[] {
  const lines = [...copy.reasons];
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
 * answers it with the clean sentence. A look that flags something must name
 * a fix, so that shape stays unreadable. */
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
    if (reasons.length > 0) throw new Error("The verdict could not be read.");
    return { lookId: look.id, reasons, fix: undefined, why: undefined };
  }
  const fix = asRecord(look.fix);
  if (!fix || typeof fix.changes !== "string" || typeof fix.why !== "string") {
    throw new Error("The verdict could not be read.");
  }
  return { lookId: look.id, reasons, fix: fix.changes, why: fix.why };
}
