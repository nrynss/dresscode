import { packageName } from "@nrynss/chaaya";
import { themeScript } from "@nrynss/chaaya/theme";
import { checkTokens } from "@nrynss/chaaya/tokens";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";
import App from "./app.svelte";

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../..");

function walk(dir: string): string[] {
  const found: string[] = [];
  for (const name of readdirSync(dir)) {
    const abs = path.join(dir, name);
    if (statSync(abs).isDirectory()) found.push(...walk(abs));
    else found.push(abs);
  }
  return found;
}

function pngSize(file: string): { width: number; height: number } {
  const bytes = readFileSync(file);
  expect(bytes.subarray(0, 8).toString("hex")).toBe("89504e470d0a1a0a");
  return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
}

test("the shell is the chaaya app", () => {
  expect(packageName).toBe("@nrynss/chaaya");
  expect(App).toBeTruthy();
});

test("theme roles are complete and the head script is chaaya's", () => {
  const css = readFileSync(path.join(here, "theme.css"), "utf8");
  expect(checkTokens(css)).toEqual([]);
  const html = readFileSync(path.join(webRoot, "index.html"), "utf8");
  expect(html).toContain(themeScript);
  expect(html).toContain('rel="manifest"');
});

/** The declaration of one custom property inside a theme block. */
function roleLines(css: string, role: string): string[] {
  return [...css.matchAll(new RegExp(`^[ \\t]*--${role}:\\s*([^;]+);`, "gm"))].map((match) => match[1].trim());
}

test("the UI font is sans-serif and the serif is the display face alone", () => {
  const css = readFileSync(path.join(here, "theme.css"), "utf8");
  // Four theme blocks, each declaring both faces: the sans-serif for every
  // control and the editorial serif for the brand and the headings.
  const ui = roleLines(css, "font-ui");
  const display = roleLines(css, "font-display");
  expect(ui).toHaveLength(4);
  expect(display).toHaveLength(4);
  for (const line of ui) {
    expect(line).toContain("system-ui");
    expect(line).not.toContain("Georgia");
  }
  for (const line of display) {
    expect(line).toContain("Georgia");
  }
  // The serif is claimed by name, for the wordmark, the headings and the two
  // dialog titles — and for nothing else, so no button or tab inherits it.
  expect(css).toMatch(
    /\.wordmark,\s*h1,\s*h2,\s*\.dresscode-title,\s*\.sheet-title\s*\{\s*font-family:\s*var\(--font-display\);\s*\}/,
  );
  expect(css.match(/font-family:\s*var\(--font-display\)/g)).toHaveLength(1);
  // The body inherits --font-ui, and nothing in the sheet sets a serif by
  // naming Georgia or a serif family outright.
  expect(css).toMatch(/body\s*\{[^}]*font-family:\s*var\(--font-ui\)/);
});

test("no file in src names a font of its own", () => {
  // The sans-everywhere claim cannot stop at theme.css: a screen that adds
  // `font-family: Georgia, serif` — or quietly redefines `--font-ui` on its
  // own — would put the newspaper face back on controls while every pin in
  // the sheet above stays green. So the sweep reads every non-test file in
  // src: each `font-family` must be one of the theme's three faces, each
  // `font` shorthand must stay `inherit`, and the three face variables may
  // be defined in theme.css alone.
  const offenders: string[] = [];
  const files = walk(path.join(webRoot, "src")).filter(
    (file) => /\.(svelte|css|ts)$/.test(file) && !file.endsWith(".test.ts"),
  );
  expect(files.length, "the source files are found").toBeGreaterThan(5);
  for (const file of files) {
    // path.relative is missing from the node types the app's tsconfig pulls
    // in, so the relative name is cut off the absolute one by hand.
    const rel = file.startsWith(webRoot) ? file.slice(webRoot.length + 1) : file;
    const text = readFileSync(file, "utf8");
    for (const match of text.matchAll(/font-family\s*:\s*([^;}"\n]+)/g)) {
      const value = match[1].trim();
      if (!/^var\(--font-(?:ui|display|numeric)\)$/.test(value)) {
        offenders.push(`font-family: ${value} in ${rel}`);
      }
    }
    for (const match of text.matchAll(/(?<![-\w])font\s*:\s*([^;}"\n]+)/g)) {
      if (match[1].trim() !== "inherit") offenders.push(`font: ${match[1].trim()} in ${rel}`);
    }
    if (!rel.endsWith("theme.css")) {
      for (const match of text.matchAll(/--font-(ui|display|numeric)\s*:/g)) {
        offenders.push(`--font-${match[1]} defined outside theme.css in ${rel}`);
      }
    }
  }
  expect(offenders, "font declarations outside the theme's three faces").toEqual([]);
});

test("the accordion recipe boxes every disclosure as a card row", () => {
  const css = readFileSync(path.join(here, "theme.css"), "utf8");
  expect(css).toMatch(/\.disclosure\s*\{[^}]*background: var\(--surface\)/);
  expect(css).toMatch(/\.disclosure\s*\{[^}]*border: 1px solid var\(--line\)/);
  expect(css).toMatch(/\.disclosure\s*\{[^}]*border-radius: var\(--radius-control\)/);
  expect(css).toMatch(/\.disclosure\s*\{[^}]*min-inline-size: 0/);
  expect(css).toMatch(/\.disclosure\s*\{[^}]*align-self: stretch/);
  expect(css).toMatch(/\.disclosure > summary\s*\{[^}]*display: flex/);
  expect(css).toMatch(/\.disclosure > summary\s*\{[^}]*justify-content: space-between/);
  expect(css).toMatch(/\.disclosure > summary\s*\{[^}]*min-height: 2\.75rem/);
  expect(css).toMatch(/\.disclosure > summary\s*\{[^}]*list-style: none/);
  expect(css).toMatch(/\.disclosure > summary::-webkit-details-marker\s*\{\s*display: none/);
  expect(css).toMatch(/\.disclosure > summary::after\s*\{\s*content: "▾"/);
  expect(css).toMatch(/\.disclosure\[open\] > summary::after\s*\{\s*rotate: 180deg/);
  expect(css).toMatch(/\.disclosure\[open\] > summary\s*\{\s*border-bottom: 1px solid var\(--line-soft\)/);
  // The account screen draws each drawer tail of its own (a `.chevron` span
  // inside its own scoped summary), so a summary carrying one must suppress
  // the recipe's tail or the card shows two. Deleting this rule keeps every
  // string above green — that is why it is pinned by name here, and again in
  // a real browser in tests/a11y/browser-shell.test.ts.
  expect(css).toMatch(/\.disclosure > summary:has\(\.chevron\)::after\s*\{\s*content: none/);
  // The one main action spans the phone's column, and the overlay control and
  // the dialog's action row take their own width back.
  expect(css).toMatch(/button\.primary,\s*\.btn-primary,\s*a\.primary-link\s*\{[^}]*width: 100%/);
  expect(css).toMatch(/\.take-btn,[\s\S]{0,60}\.dresscode-panel \.row > button\s*\{\s*width: auto/);
});

test("the manifest is installable and the icons are the sizes it names", () => {
  const manifest = JSON.parse(readFileSync(path.join(webRoot, "static/manifest.webmanifest"), "utf8")) as {
    name: string;
    short_name: string;
    start_url: string;
    display: string;
    icons: { src: string; sizes: string; purpose?: string; type: string }[];
  };
  expect(manifest.name).toBe("Dresscode");
  expect(manifest.short_name).toBe("Dresscode");
  expect(manifest.start_url).toBe("/");
  expect(manifest.display).toBe("standalone");
  const bySize = new Map(manifest.icons.map((icon) => [icon.sizes, icon]));
  expect(bySize.get("192x192")?.type).toBe("image/png");
  expect(bySize.get("512x512")?.type).toBe("image/png");
  expect(manifest.icons.some((icon) => icon.purpose === "maskable")).toBe(true);

  expect(pngSize(path.join(webRoot, "static/icons/icon-192.png"))).toEqual({ width: 192, height: 192 });
  expect(pngSize(path.join(webRoot, "static/icons/icon-512.png"))).toEqual({ width: 512, height: 512 });
  expect(pngSize(path.join(webRoot, "static/icons/icon-maskable-512.png"))).toEqual({ width: 512, height: 512 });
  expect(pngSize(path.join(webRoot, "static/icons/apple-touch-icon.png"))).toEqual({ width: 180, height: 180 });
});

test("the shell copy does not grade a person", () => {
  const banned = /\b(score|probability|percent|gender|attractiveness|ratio)\b/i;
  const files = walk(path.join(webRoot, "src")).filter((file) => /\.(svelte|css|ts)$/.test(file) && !file.endsWith(".test.ts"));
  expect(files.length).toBeGreaterThan(0);
  for (const file of files) {
    expect(readFileSync(file, "utf8"), file).not.toMatch(banned);
  }
});
