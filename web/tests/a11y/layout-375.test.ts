import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";
import { phoneWidth, themeCss } from "./helpers";

const here = path.dirname(fileURLToPath(import.meta.url));
const sheetCss = readFileSync(path.resolve(here, "../../src/routes/look/render/sheet.svelte"), "utf8");

/** jsdom lays nothing out, so the phone width cannot be measured by reflow.
 * What a static gate can assert honestly is the two rules the 375 px clause
 * rests on: the shell and the sheet take the full width when the screen is
 * narrower than their cap, and no rule pins a fixed width wider than the
 * phone, so nothing can push a line past the edge. */
test("the shell and the sheet take the full width at 375 px", () => {
  expect(themeCss).toMatch(/\.shell\s*\{[^}]*width:\s*min\(40rem,\s*100%\)/);
  expect(sheetCss).toMatch(/width:\s*min\(40rem,\s*100%\)/);
  expect(40 * 16).toBeGreaterThan(phoneWidth);
});

test("no style rule pins a fixed width wider than the phone", () => {
  const pinned: string[] = [];
  const rules = [themeCss, sheetCss];
  for (const rule of rules) {
    for (const match of rule.matchAll(/(min-width|max-width|width)\s*:\s*([0-9.]+)px/g)) {
      if (Number.parseFloat(match[2]) > phoneWidth) pinned.push(match[0]);
    }
  }
  expect(pinned, "fixed widths wider than the phone").toEqual([]);
});
