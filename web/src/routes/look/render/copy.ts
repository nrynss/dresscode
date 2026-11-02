/** The screen's sentences and the readers that turn recorded bodies into
 * them. A failed render is never read as a render, whatever its check said
 * about the parts a picture cannot carry. */

export const openingSentence = "Opening this look.";

export const pendingSentence = "This look is still being finished. Open the verdict to follow it.";

/** Said when every render was refused. It leads with what the user still
 * gets, not with a failure: the fix and the products below stand. */
export const suggestionSentence =
  "Here is the change to make. The try-on picture did not keep your likeness, so we show your original instead.";

/** The look that flags nothing is the good outcome: there is no fix to name,
 * so the screen says so instead of throwing. */
export const cleanSentence = "Nothing needs changing; the outfit already meets the dress code.";

export const shopSentence = "Every product is labelled for what it is.";

export const shopOpeningSentence = "Opening the shop list.";

export const shopEmptySentence = "Nothing is in the shop list yet.";

/** The look's verdict. One field, one stored value, three words. */
export const lookVerdicts = ["kept", "dismissed", "bought"] as const;

export type LookVerdict = (typeof lookVerdicts)[number];

export function isLookVerdict(value: string): value is LookVerdict {
  return (lookVerdicts as readonly string[]).includes(value);
}

/** The action words. They are words, never a number. */
export const verdictWords: Record<LookVerdict, string> = {
  kept: "Keep",
  dismissed: "Dismiss",
  bought: "Bought it",
};

/** What the screen says once the server has stored the verdict. */
export const savedSentences: Record<LookVerdict, string> = {
  kept: "This look is kept.",
  dismissed: "This look is dismissed.",
  bought: "You bought this look.",
};

/** The fidelity words the domain uses. They are words, never a number. */
export const fidelityLabels = ["exact product", "approximate match", "visual simulation"] as const;

export type FidelityLabel = (typeof fidelityLabels)[number];

export function isFidelityLabel(value: string): value is FidelityLabel {
  return (fidelityLabels as readonly string[]).includes(value);
}

/** One plain sentence per fidelity word: what the label on a shop product
 * promises. The sheet opens it beside the label that names it. */
export const fidelityExplanations: Record<FidelityLabel, string> = {
  "exact product": "This is the exact piece you brought or the occasion named, tried on directly.",
  "approximate match": "This piece is a close catalog match for the fix shown in the render.",
  "visual simulation": "The render shows the effect on your photo, paired with a shopping suggestion.",
};

/** One shown render: the picture and nothing that could grade it. */
export interface RenderPicture {
  url: string;
}

/** The lines this screen shows. The look id and the check booleans are not among them.
 * `fix` and `why` are undefined when the judge flagged nothing: the outfit already
 * meets the dress code, and the screen says so instead of a fix that does not exist. */
export interface RenderCopy {
  lookId: string;
  reasons: string[];
  fix: string | undefined;
  why: string | undefined;
  selfieUrl: string;
  /** Undefined when no render passed the check. A failed render never lands here. */
  render: RenderPicture | undefined;
}

export interface ShopProduct {
  id: string;
  name: string;
  label: FidelityLabel;
  url: string | undefined;
}

export interface ShopList {
  products: ShopProduct[];
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/** The three booleans of the render check, all true. Anything else is a
 * render the screen may not show. */
function checkPassed(check: unknown): boolean {
  const record = asRecord(check);
  if (!record) return false;
  return record.change_made === true && record.same_person === true && record.criterion_met === true;
}

/** The render the screen may show, or undefined. A render whose check did not
 * pass all three is discarded here, so it cannot reach a picture frame. */
function shownRender(shown: unknown): RenderPicture | undefined {
  if (shown === null || shown === undefined) return undefined;
  const record = asRecord(shown);
  const url = record === undefined ? undefined : asString(record.url);
  if (!url || !checkPassed(record?.check)) return undefined;
  return { url };
}

/** The ready look, or null while the job is still running.
 * A body that cannot be read at all throws. A look whose judge flagged nothing
 * is read whole: its fix key rides null (the server marshals the absent pick
 * that way) or is absent, and the screen answers it with the clean sentence. */
export function renderFromLook(body: unknown): RenderCopy | null {
  const look = asRecord(body);
  if (!look) throw new Error("The render could not be read.");
  if (look.ready !== true) return null;
  const lookId = asString(look.id);
  const selfieUrl = asString(look.selfie_url);
  if (lookId === undefined || selfieUrl === undefined) throw new Error("The render could not be read.");
  const flags = look.flags;
  if (!Array.isArray(flags)) throw new Error("The render could not be read.");
  const reasons: string[] = [];
  for (const item of flags) {
    const flag = asRecord(item);
    const suggestion = flag === undefined ? undefined : asString(flag.suggestion);
    if (suggestion === undefined) throw new Error("The render could not be read.");
    reasons.push(suggestion);
  }
  // No fix, and no reason named either: the good outcome. A look that flags
  // something must name a fix, so that shape stays unreadable. Nor can a
  // clean look carry a render: there was no fix to render.
  if (look.fix === null || look.fix === undefined) {
    if (reasons.length > 0) throw new Error("The render could not be read.");
    return { lookId, reasons, fix: undefined, why: undefined, selfieUrl, render: undefined };
  }
  const fix = asRecord(look.fix);
  const changes = fix === undefined ? undefined : asString(fix.changes);
  const why = fix === undefined ? undefined : asString(fix.why);
  if (changes === undefined || why === undefined) throw new Error("The render could not be read.");
  return { lookId, reasons, fix: changes, why, selfieUrl, render: shownRender(look.shown_render) };
}

/** The shop list, every product with its label spelled the domain's way. */
export function shopFromResponse(body: unknown): ShopList {
  const record = asRecord(body);
  const products = record?.products;
  if (!record || !Array.isArray(products)) throw new Error("The shop list could not be read.");
  const list: ShopProduct[] = [];
  for (const item of products) {
    const product = asRecord(item);
    if (product === undefined) throw new Error("The shop list could not be read.");
    const id = asString(product.id);
    const name = asString(product.name);
    const fidelity = asString(product.fidelity);
    if (id === undefined || name === undefined || fidelity === undefined || !isFidelityLabel(fidelity)) {
      throw new Error("The shop list could not be read.");
    }
    const url = asString(product.url);
    list.push({ id, name, label: fidelity, url });
  }
  return { products: list };
}
