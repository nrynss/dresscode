import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";
import { gate, mountApp, styleBlocks, themeCss, useMock, waitUntil, type Mode } from "./helpers";

const here = path.dirname(fileURLToPath(import.meta.url));

const modes: Mode[] = ["light", "dark"];

/** The four tabs, in the order the bar lays them out. `href` is the hash the
 * tab leads to, which is the hash that names it current. */
const tabs = [
  { label: "Occasion", href: "#/" },
  { label: "Mirror", href: "#/capture" },
  { label: "History", href: "#/history" },
  { label: "Account", href: "#account" },
] as const;

interface NavState {
  /** The hash the app is mounted on. */
  hash: string;
  /** The tab that must carry `aria-current="page"`, or "" when the route sits
   * past the four and the back button is the way out. */
  current: string;
  /** The hash the current tab's own href must be — the route it leads to. */
  leadsTo: string;
  /** Whether the top bar shows the circular back link. */
  back: boolean;
}

/** The routes the tab bar and the back button answer to, in every spelling a
 * hash can take: the canonical form and the bare word that app.svelte's
 * currentRoute() folds onto it. Each row pins the tab that must light, so a
 * route whose screen renders with no tab lit (`#history`, before the
 * normalization) cannot come back. */
const states: NavState[] = [
  { hash: "", current: "Occasion", leadsTo: "#/", back: false },
  { hash: "#/", current: "Occasion", leadsTo: "#/", back: false },
  { hash: "#/occasion", current: "Occasion", leadsTo: "#/", back: true },
  { hash: "#occasion", current: "Occasion", leadsTo: "#/", back: true },
  { hash: "#/capture", current: "Mirror", leadsTo: "#/capture", back: true },
  { hash: "#/history", current: "History", leadsTo: "#/history", back: true },
  { hash: "#history", current: "History", leadsTo: "#/history", back: true },
  { hash: "#account", current: "Account", leadsTo: "#account", back: true },
  { hash: "#home", current: "Occasion", leadsTo: "#/", back: true },
  { hash: "#capture", current: "Occasion", leadsTo: "#/", back: true },
  { hash: "#/look/verdict", current: "", leadsTo: "", back: true },
  { hash: "#look/verdict", current: "", leadsTo: "", back: true },
  { hash: "#/look/render", current: "", leadsTo: "", back: true },
];

function tabElements(): Element[] {
  return [...document.querySelectorAll(".tab-bar a.tab")];
}

function tabByLabel(label: string): Element | null {
  return tabElements().find((tab) => tab.textContent?.trim() === label) ?? null;
}

for (const mode of modes) {
  for (const state of states) {
    test(
      `the tab bar and the back button read the ${state.hash || "empty"} route, ${mode}, 375 px`,
      { timeout: 30000 },
      async () => {
        useMock();
        const leave = await mountApp(mode, state.hash);
        try {
          await waitUntil(() => document.querySelector(".tab-bar"), "the fixed tab bar");
          const onBar = tabElements();
          expect(onBar.map((tab) => tab.textContent?.trim()), "the four tabs, in order").toEqual([...tabs].map((t) => t.label));
          expect(onBar.map((tab) => tab.getAttribute("href")), "each tab names its route").toEqual(
            tabs.map((tab) => tab.href),
          );
          // Every tab carries an SVG icon and a label, and the icon is the
          // decoration: the label is the accessible name.
          for (const tab of onBar) {
            const icon = tab.querySelector("svg");
            expect(icon, "the tab's SVG icon").not.toBeNull();
            expect(icon?.getAttribute("aria-hidden"), "the icon is hidden from readers").toBe("true");
          }
          // Every tab that names itself current, not just the first one: two
          // `aria-current="page"` markers on one bar is a broken bar, and a
          // single-element query would read only the first and pass it.
          const currents = [...document.querySelectorAll('.tab-bar a[aria-current="page"]')];
          if (state.current === "") {
            expect(currents, "a route past the four tabs names no tab").toHaveLength(0);
          } else {
            expect(currents, "exactly one tab is current").toHaveLength(1);
            const current = currents[0]!;
            expect(current.textContent?.trim(), "the current tab").toBe(state.current);
            expect(current.getAttribute("href"), "the current tab leads where the route is").toBe(state.leadsTo);
            expect(tabByLabel(state.current)?.getAttribute("aria-current"), "the tab is current").toBe("page");
          }
          const back = document.querySelector("header.bar a.icon-back");
          if (state.back) {
            expect(back, "a sub-route shows the back link").not.toBeNull();
            expect(back?.getAttribute("href"), "back goes to the front door").toBe("#/");
            expect(back?.getAttribute("aria-label"), "the icon-only link is named").toBe("Back");
          } else {
            expect(back, "the front door shows no back link").toBeNull();
          }
          await gate(mode);
        } finally {
          await leave();
        }
      },
    );
  }
}

/** The bar is `position: fixed` across the viewport, so a wide label or a
 * round number cannot push it past 375 px: it is the four-column grid, capped
 * at the reading width and made of columns that can all shrink below their
 * content, that holds it. jsdom lays nothing out, so this is the rule the
 * phone-width clause rests on; the width itself is measured in a real browser. */
test("the fixed tab bar is built to stay inside the phone column", () => {
  const shell = readFileSync(path.resolve(here, "../../src/app/shell.svelte"), "utf8");
  const styles = styleBlocks(shell).join("\n");
  expect(styles).toMatch(/\.tab-bar\s*\{[^}]*position: fixed/);
  expect(styles).toMatch(/\.tab-bar\s*\{[^}]*left: 0;\s*right: 0/);
  expect(styles).toMatch(/\.tab-bar\s*\{[^}]*env\(safe-area-inset-left\)/);
  expect(styles).toMatch(/\.tab-bar\s*\{[^}]*env\(safe-area-inset-bottom\)/);
  expect(styles).toMatch(/\.tab-grid\s*\{[^}]*max-width: 40rem/);
  expect(styles).toMatch(/\.tab-grid\s*\{[^}]*grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/);
  // A tab label breaks inside its column instead of widening it.
  expect(styles).toMatch(/\.tab span\s*\{[^}]*overflow-wrap: anywhere/);
  // The bar draws no width of its own; it spans the viewport with left/right.
  expect(shell).not.toMatch(/\.tab-bar\s*\{[^}]*width:/);
  // And the shell's last measure clears the bar, so no content sits under it.
  expect(themeCss).toMatch(/\.shell\s*\{[^}]*calc\(5\.25rem \+ env\(safe-area-inset-bottom\)\)/);
});
