/** Screen words for a look. Shop, and any other stage, has none. */
const stageWords: Record<string, string> = {
  judge: "Checking",
  pick: "Choosing a fix",
  measure: "Choosing a fix",
  render: "Rendering",
  check: "Checking the render",
};

export const pausedSentence = "This look waits while Dresscode is in the background.";

export const openingSentence = "Opening this look.";

export const readingSentence = "Reading the verdict.";

export function stageWord(stage: string | undefined): string {
  if (stage === undefined || stage === "") return "";
  return stageWords[stage] ?? "";
}

/** The lines the verdict screen shows. The look id is not one of them. */
export interface VerdictCopy {
  lookId: string;
  reasons: string[];
  fix: string;
  why: string;
}

export function verdictLines(copy: VerdictCopy): string[] {
  return [...copy.reasons, copy.fix, copy.why];
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

/** Reasons, the suggested fix, and why. A pending look is not a verdict. */
export function verdictFromLook(body: unknown): VerdictCopy {
  const look = asRecord(body);
  const fix = asRecord(look?.fix);
  const flags = look?.flags;
  if (!look || look.ready !== true || typeof look.id !== "string" || !fix || !Array.isArray(flags)) {
    throw new Error("The verdict could not be read.");
  }
  if (typeof fix.changes !== "string" || typeof fix.why !== "string") {
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
  return { lookId: look.id, reasons, fix: fix.changes, why: fix.why };
}
