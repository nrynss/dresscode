import { theme } from "@nrynss/chaaya/theme";
import { a11yGate } from "@nrynss/chaaya/testing";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { mount, unmount } from "svelte";
import { expect, vi } from "vitest";
import App from "../../src/app/app.svelte";
import { installMock, resetMockChoices } from "../../src/lib/api/mock";

const here = path.dirname(fileURLToPath(import.meta.url));

/** The stylesheet the contrast gate reads and the mounted screens wear. */
export const themeCss = readFileSync(path.resolve(here, "../../src/app/theme.css"), "utf8");

/** The app's own source, the root the static guards sweep. */
export const srcRoot = path.resolve(here, "../../src");

/** The width the gates run at: a small phone. */
export const phoneWidth = 375;

/** Which forced theme the document wears. */
export type Mode = "light" | "dark";

let mockInstalled = false;

/** Serve the recorded API for the whole file, with fresh pins each test. */
export function useMock(): void {
  if (!mockInstalled) {
    installMock();
    mockInstalled = true;
  }
  resetMockChoices();
}

let style: HTMLStyleElement | undefined;

/** Mount the whole app on the page body: shell, theme control, and the route
 * the hash names, in the forced theme, at the phone width. The returned
 * function unmounts it again. */
export async function mountApp(mode: Mode, hash: string): Promise<() => Promise<void>> {
  window.location.hash = hash;
  document.body.innerHTML = "";
  style?.remove();
  style = document.createElement("style");
  style.textContent = themeCss;
  document.head.append(style);
  theme.set(mode);
  expect(document.documentElement.dataset.theme, `the ${mode} theme is forced`).toBe(mode);
  window.innerWidth = phoneWidth;
  window.innerHeight = 667;
  const app = mount(App, { target: document.body });
  return async () => {
    await unmount(app);
    document.body.innerHTML = "";
  };
}

/** The first element whose trimmed text is exactly the sentence. */
export function withText<E extends Element = Element>(selector: string, text: string): E | null {
  for (const element of document.querySelectorAll(selector)) {
    if (element.textContent?.trim() === text) return element as E;
  }
  return null;
}

/** Wait until the marker element is on the page, so a screen is judged in the
 * state a reader would see, not its empty opening frame. */
export async function waitUntil(marker: () => Element | null, label: string): Promise<void> {
  await vi.waitFor(
    () => {
      expect(marker(), label).not.toBeNull();
    },
    { timeout: 10000, interval: 25 },
  );
}

function firstInputOf(label: string): HTMLInputElement {
  const holder = withText("label", label);
  expect(holder, `the ${label} control`).not.toBeNull();
  const input = holder?.querySelector("input");
  expect(input, `the ${label} input`).not.toBeNull();
  return input as HTMLInputElement;
}

/** Run Chaaya's accessibility gate over the whole rendered page. */
export async function gate(mode: Mode): Promise<void> {
  expect(document.querySelector(".shell"), "the shell is mounted").not.toBeNull();
  expect(document.documentElement.dataset.theme, `the ${mode} theme is forced`).toBe(mode);
  expect(window.innerWidth, "the gates run at the phone width").toBe(phoneWidth);
  await a11yGate(document.body);
}

/** Open the occasion screen's fix menu for the recorded occasion. */
export async function openFixMenu(): Promise<void> {
  const occasion = firstInputOf("Family function");
  occasion.click();
  await waitUntil(() => withText("legend", "Fixes you are open to"), "the fix menu");
}

/** Open the front door's sample-look drawer, so the judge buttons face the
 * gate the way a reader who wants a sample sees them. */
export async function openSampleDrawer(): Promise<void> {
  const summary = withText("summary", "Try a sample look");
  expect(summary, "the sample drawer summary").not.toBeNull();
  (summary as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.samples[open]"), "the sample drawer opens");
}

/** Tick one fix pill by its compact label: the input's own change event is
 * the tap the picker's auto-save answers. */
export function toggleFixPill(label: string): void {
  const input = firstInputOf(label);
  input.click();
}

export interface WriteCall {
  method: string;
  path: string;
  body: unknown;
}

/** Watch the writes the screens send while the gate runs. The decluttered
 * picker no longer draws a confirmation, so its auto-save is proven on the
 * wire, not by a marker on the page. */
export function watchWrites(): { writes: WriteCall[]; stop: () => void } {
  const writes: WriteCall[] = [];
  const inner = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const url = new URL(raw, "http://dresscode.local");
    let body: unknown;
    if (typeof init?.body === "string" && init.body !== "") body = JSON.parse(init.body) as unknown;
    if (method !== "GET") writes.push({ method, path: url.pathname + url.search, body });
    return inner(input, init);
  }) as typeof fetch;
  return {
    writes,
    stop() {
      globalThis.fetch = inner;
    },
  };
}

/** Open the shop-the-look bottom sheet. */
export async function openShopSheet(): Promise<void> {
  const trigger = withText("button", "Shop the look");
  expect(trigger, "the shop trigger").not.toBeNull();
  (trigger as HTMLElement).click();
  await waitUntil(() => withText("h3", "Gold jhumkas"), "the product in the sheet");
}

/** Open the capture screen's product drawer, so the fields inside it face the
 * gate the way a reader who is considering a piece sees them. */
export async function openProductDrawer(): Promise<void> {
  const summary = withText("summary", "Considering a specific piece?");
  expect(summary, "the product drawer summary").not.toBeNull();
  (summary as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.product-drawer[open]"), "the product drawer opens");
}

/** Open the finished verdict's stepper drawer, so the completed steps face
 * the gate the way a reader who opens them sees them. */
export async function openStagesDrawer(): Promise<void> {
  const summary = withText("summary", "Checked against the dress code");
  expect(summary, "the stages drawer summary").not.toBeNull();
  (summary as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.stages-drawer[open]"), "the stages drawer opens");
}

/** Open the missed-criteria drawer on a look screen, so the dress-code notes
 * face the gate the way a reader who expands them sees them. */
export async function openReasonsDrawer(): Promise<void> {
  const summary = withText("summary", "What the dress code still asks for");
  expect(summary, "the reasons drawer summary").not.toBeNull();
  (summary as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.reasons-drawer[open]"), "the reasons drawer opens");
}

/** Open a fidelity explanation beside its label in the open shop sheet: tap
 * the product's label chip and wait for the named explanation popover. */
export async function openFidelityExplanation(): Promise<void> {
  const trigger = withText("button", "exact product");
  expect(trigger, "the fidelity label in the sheet").not.toBeNull();
  (trigger as HTMLElement).click();
  await waitUntil(() => {
    const pop = document.querySelector('[aria-label="What the label means"]');
    return pop !== null && (pop.textContent ?? "").trim() !== "" ? pop : null;
  }, "the fidelity explanation");
}

/** Turn the style history switch on and wait for the second visit. */
export async function openSecondVisit(): Promise<void> {
  const historySwitch = firstInputOf("Style history");
  historySwitch.click();
  await waitUntil(
    () => withText("p", "Last family function you kept the gold jhumkas and dismissed the red lip."),
    "the recall sentence",
  );
}

/** Open the account screen's who-sees-what disclosure, so the privacy table
 * faces the gate the way a reader who expands it sees it. The disclosure is
 * closed by default and its summary names itself with the heading inside. */
export async function openPrivacyDrawer(): Promise<void> {
  const heading = withText("h2", "Who sees what");
  expect(heading, "the who-sees-what heading").not.toBeNull();
  (heading!.closest("summary") as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.privacy-drawer[open]"), "the who-sees-what disclosure opens");
}

/** Open the account screen's erasure disclosure, so the deletion sentences
 * and the fresh-code button face the gate the way a reader who expands it sees
 * them. The summary names itself with the heading inside, so the heading is
 * the marker and its summary is the tap. */
export async function openEraseDrawer(): Promise<void> {
  const heading = withText("h3", "Delete the account");
  expect(heading, "the delete-the-account heading").not.toBeNull();
  (heading!.closest("summary") as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.erase-drawer[open]"), "the erasure disclosure opens");
}

/** Open the history screen's notice disclosure, so the explanation of what the
 * switch keeps faces the gate the way a reader who expands it sees it. */
export async function openHistoryNotice(): Promise<void> {
  const summary = withText("summary", "How style history works");
  expect(summary, "the history notice drawer summary").not.toBeNull();
  (summary as HTMLElement).click();
  await waitUntil(() => document.querySelector("details.notice-drawer[open]"), "the history notice disclosure opens");
}

/** Open a confirm dialog on the account screen. */
export async function openDeleteDialog(title: string): Promise<void> {
  const trigger = withText("button", title);
  expect(trigger, `the ${title} trigger`).not.toBeNull();
  (trigger as HTMLElement).click();
  await waitUntil(() => document.querySelector('[role="dialog"]'), `the ${title} dialog`);
}

/** Open the home screen's about dialog. */
export async function openAboutDialog(): Promise<void> {
  const trigger = withText("button", "About");
  expect(trigger, "the about trigger").not.toBeNull();
  (trigger as HTMLElement).click();
  await waitUntil(() => document.querySelector('[role="dialog"]'), "the about dialog");
}

function walkSvelte(dir: string): string[] {
  const found: string[] = [];
  for (const name of readdirSync(dir)) {
    const abs = path.join(dir, name);
    if (statSync(abs).isDirectory()) found.push(...walkSvelte(abs));
    else if (name.endsWith(".svelte")) found.push(abs);
  }
  return found;
}

export interface ScreenSource {
  /** The path under `src`, for naming a file a guard fails on. */
  file: string;
  /** The whole file, markup and style blocks alike. */
  text: string;
}

/** Every .svelte screen — the shell's directory and the routes — with its
 * whole text, so the static guards sweep the same files the mounted gates
 * judge. */
export function screenSources(): ScreenSource[] {
  const files = walkSvelte(path.join(srcRoot, "app")).concat(walkSvelte(path.join(srcRoot, "routes")));
  return files.map((file) => ({ file: path.relative(srcRoot, file), text: readFileSync(file, "utf8") }));
}

/** The `<style>` blocks of one screen's text. */
export function styleBlocks(text: string): string[] {
  const styles: string[] = [];
  for (const match of text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)) {
    styles.push(match[1]);
  }
  return styles;
}
