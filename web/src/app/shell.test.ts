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
