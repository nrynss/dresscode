import { contrastGate, type ContrastPair } from "@nrynss/chaaya/testing";
import { expect, test } from "vitest";
import { screenSources, styleBlocks, themeCss } from "./helpers";

/** The colour roles on Chaaya's token contract. The other roles carry radii,
 * fonts, and the focus ring's width, which no pair measures. */
const colourRoles = [
  "ground",
  "surface",
  "raised",
  "sunken",
  "line",
  "line-soft",
  "text",
  "dim",
  "faint",
  "accent",
  "accent-soft",
  "on-accent",
  "ok",
  "ok-soft",
  "warn",
  "warn-soft",
  "stop",
  "stop-soft",
] as const;

/** Every text pair the screens draw, foreground over background. The gate
 * measures each pair in every theme block the stylesheet defines. */
const textPairs: readonly ContrastPair[] = [
  ["text", "ground"],
  ["text", "surface"],
  ["text", "raised"],
  ["text", "sunken"],
  ["text", "stop-soft"],
  ["text", "accent-soft"],
  ["dim", "ground"],
  ["dim", "surface"],
  ["accent", "ground"],
  ["on-accent", "accent"],
  ["ok", "ground"],
];

interface CssRule {
  selector: string;
  body: string;
}

/** Split a stylesheet into its rules, so the four theme blocks can be told
 * apart from the element rules that follow them. */
function parseRules(css: string): CssRule[] {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules: CssRule[] = [];
  let at = 0;
  function readBody(): string {
    let depth = 0;
    const start = at;
    while (at < text.length) {
      const char = text[at];
      if (char === "{") depth += 1;
      else if (char === "}") {
        if (depth === 0) {
          const body = text.slice(start, at);
          at += 1;
          return body;
        }
        depth -= 1;
      }
      at += 1;
    }
    return text.slice(start);
  }
  function readRules(): void {
    let prelude = "";
    while (at < text.length) {
      const char = text[at];
      if (char === "{") {
        const selector = prelude.trim();
        prelude = "";
        at += 1;
        if (selector.startsWith("@")) readRules();
        else rules.push({ selector, body: readBody() });
      } else if (char === "}") {
        at += 1;
        return;
      } else if (char === ";") {
        prelude = "";
        at += 1;
      } else {
        prelude += char;
        at += 1;
      }
    }
  }
  readRules();
  return rules;
}

const colourDeclaration = /^(color|background|background-color|border|border-color|border-[a-z-]+-color|accent-color|outline|outline-color|box-shadow|text-decoration-color|fill|stroke)$/;

interface Drawn {
  prop: string;
  value: string;
}

function declarations(css: string): Drawn[] {
  const drawn: Drawn[] = [];
  // A declaration opens at the start of the text or after `{` or `;`, and
  // closes before the next `;`, the closing `}`, or the end of the text — the
  // last declaration of a block may carry no trailing semicolon.
  const pattern = /(?:^|[{;])\s*([a-z-]+)\s*:\s*([^;{}]+)(?=[;}]|$)/g;
  for (const match of css.matchAll(pattern)) {
    const prop = match[1];
    if (colourDeclaration.test(prop)) drawn.push({ prop, value: match[2].trim() });
  }
  return drawn;
}


/** One role named on its own, with nothing else in the value. */
function pureRole(value: string): string | undefined {
  const role = /^var\(--([a-z0-9-]+)\)$/.exec(value);
  return role === null ? undefined : role[1];
}

/** A value draws colour only through theme roles. Words that carry no colour
 * ride along inside the allowed set, and a size rides beside the colour it
 * qualifies (a border's width), so a number with a unit is stripped too;
 * anything left must be punctuation or a number, so a raw hex, an rgb call,
 * or a colour name fails here. */
function drawsOnlyRoles(value: string): boolean {
  const withoutVars = value.replace(/var\(--[a-z0-9-]+(?:\s*,[^)]*)?\)/g, " ");
  const withoutWords = withoutVars.replace(
    /\b(?:transparent|none|currentColor|inherit|initial|color-mix|in|srgb|auto|solid)\b/g,
    " ",
  );
  const withoutSizes = withoutWords.replace(/\d*\.?\d+(?:px|em|rem|vh|vw|dvh|svh|deg|ms|s|fr|ch|ex)?\b/g, " ");
  return !/[^%,()\s.0-9-]/.test(withoutSizes);
}

interface SvgPaint {
  attr: string;
  value: string;
  file: string;
}

/** The svg paint attributes the screens' markup carries: `fill` and `stroke`.
 * A colour there draws like a colour anywhere else, so it faces the same
 * allowlist as a CSS declaration. */
function svgPaints(): SvgPaint[] {
  const paints: SvgPaint[] = [];
  for (const { file, text } of screenSources()) {
    for (const match of text.matchAll(/\b(fill|stroke)\s*=\s*(?:"([^"]*)"|'([^']*)')/g)) {
      paints.push({ attr: match[1], value: match[2] ?? match[3], file });
    }
  }
  return paints;
}

/** Every colour declaration the app draws outside the theme blocks: the shell's
 * element rules and each screen's own styles. */
function drawnColours(): Drawn[] {
  const elementRules = parseRules(themeCss)
    .filter((rule) => !rule.body.includes("color-scheme"))
    .map((rule) => rule.body);
  const screens = screenSources();
  expect(screens.length, "the screens are found").toBeGreaterThan(5);
  return [...elementRules, ...screens.flatMap((screen) => styleBlocks(screen.text))].flatMap((body) =>
    declarations(body),
  );
}

test("the stylesheet carries all four theme blocks, so the gate measures light and dark", () => {
  expect(themeCss.match(/^[ \t]*color-scheme:/gm)?.length).toBe(4);
  expect(themeCss).toContain(":root");
  expect(themeCss).toContain("(prefers-color-scheme: dark)");
  expect(themeCss).toContain('[data-theme="dark"]');
  expect(themeCss).toContain('[data-theme="light"]');
});

test("every drawn text pair reaches the AA minimum in all four theme blocks", () => {
  for (const pair of textPairs) {
    for (const role of pair) {
      expect(colourRoles as readonly string[]).toContain(role);
    }
  }
  contrastGate(themeCss, textPairs);
});

test("the screens draw no colour the theme does not define", () => {
  const raw: string[] = [];
  for (const { value } of drawnColours()) {
    if (!drawsOnlyRoles(value)) raw.push(value);
  }
  const svgRaw: string[] = [];
  for (const { attr, value, file } of svgPaints()) {
    if (!drawsOnlyRoles(value)) svgRaw.push(`${attr}="${value}" in ${file}`);
  }
  expect(raw, "colour values that are not theme roles").toEqual([]);
  expect(svgRaw, "svg paint attributes that are not theme roles").toEqual([]);
});

test("every colour drawn as text or background sits in a measured pair", () => {
  const foregrounds = new Set(textPairs.map(([role]) => role));
  const backgrounds = new Set(textPairs.map(([, role]) => role));
  const missingForeground = new Set<string>();
  const missingBackground = new Set<string>();
  for (const { prop, value } of drawnColours()) {
    const role = pureRole(value);
    if (role === undefined || !(colourRoles as readonly string[]).includes(role)) continue;
    if (prop === "color" && !foregrounds.has(role)) missingForeground.add(role);
    if ((prop === "background" || prop === "background-color") && !backgrounds.has(role)) {
      missingBackground.add(role);
    }
  }
  expect([...missingForeground], "foreground roles with no measured pair").toEqual([]);
  expect([...missingBackground], "background roles with no measured pair").toEqual([]);
});
