import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test } from "vitest";
import { humanMessage } from "../../../lib/api/errors";
import { installMock, lastFixtureFile, pinAnswer, preferFixture } from "../../../lib/api/mock";
import { clampedPosition, midPosition, originalPosition, renderPosition } from "./compare";
import {
  fidelityExplanations,
  fidelityLabels,
  isFidelityLabel,
  isLookVerdict,
  lookVerdicts,
  openingSentence,
  pendingSentence,
  savedSentences,
  shopEmptySentence,
  shopOpeningSentence,
  shopSentence,
  suggestionSentence,
  verdictWords,
  renderFromLook,
  shopFromResponse,
} from "./copy";
import { loadLook, loadShop, sendVerdict, shownFailure, storyLookId } from "./load";
import { isRenderHash, renderHash } from "./route";

const here = path.dirname(fileURLToPath(import.meta.url));
const apiDir = path.resolve(here, "../../../../../testdata/api");

const suggestionLookId = "100c0000000000000000000000000002";

interface RecordedFile {
  method?: string;
  path?: string;
  status?: number;
  body?: unknown;
}

function disk(file: string): RecordedFile {
  return JSON.parse(readFileSync(path.join(apiDir, file), "utf8")) as RecordedFile;
}

/** The static words of a screen: its markup without the style block, the
 * expressions, and the tags. What a reader would see. */
function staticText(screen: string): string {
  const markup = screen.slice(screen.indexOf("</script>")).replace(/<style>[\s\S]*<\/style>/, "");
  return markup
    .replace(/\{#[^}]*\}/g, " ")
    .replace(/\{:[^}]*\}/g, " ")
    .replace(/\{\/[^}]*\}/g, " ")
    .replace(/\{[^}]*\}/g, " ")
    .replace(/<[^>]+>/g, " ");
}

function styleBlock(screen: string): string {
  const match = /<style>[\s\S]*<\/style>/.exec(screen);
  return match === null ? "" : match[0];
}

afterEach(() => {
  installMock()();
});

test("the screen's sentences and words carry no digit", () => {
  const words = [
    openingSentence,
    pendingSentence,
    suggestionSentence,
    shopSentence,
    shopOpeningSentence,
    shopEmptySentence,
    ...Object.values(verdictWords),
    ...Object.values(savedSentences),
    ...fidelityLabels,
  ].join(" ");
  expect(words).not.toMatch(/\d/);
  expect(fidelityLabels).toEqual(["exact product", "approximate match", "visual simulation"]);
  expect(isFidelityLabel("exact product")).toBe(true);
  expect(isFidelityLabel("approximate match")).toBe(true);
  expect(isFidelityLabel("visual simulation")).toBe(true);
  expect(isFidelityLabel("exact")).toBe(false);
  expect(lookVerdicts).toEqual(["kept", "dismissed", "bought"]);
  expect(isLookVerdict("kept")).toBe(true);
  expect(isLookVerdict("seen")).toBe(false);
  // The bought sentence speaks to the reader, not to the record.
  expect(savedSentences.bought).toBe("You bought this look.");
  // One plain sentence per fidelity word, in the sheet's own explanation.
  expect(fidelityExplanations["exact product"]).toBe(
    "This is the exact piece you brought or the occasion named, tried on directly.",
  );
  expect(fidelityExplanations["approximate match"]).toBe(
    "This piece is a close catalog match for the fix shown in the render.",
  );
  expect(fidelityExplanations["visual simulation"]).toBe(
    "The render shows the effect on your photo, paired with a shopping suggestion.",
  );
  expect(Object.values(fidelityExplanations).join(" ")).not.toMatch(/\d/);
  const recorded = [disk("shop.json"), disk("shop-none.json"), disk("shop-simulation.json")];
  for (const file of recorded) {
    for (const product of (file.body as { products: { name: string; fidelity: string }[] }).products) {
      expect(`${product.name} ${product.fidelity}`).not.toMatch(/\d/);
      expect(isFidelityLabel(product.fidelity)).toBe(true);
    }
  }
});

test("the route answers to its hash", () => {
  expect(renderHash).toBe("#/look/render");
  expect(isRenderHash("#/look/render")).toBe(true);
  expect(isRenderHash("/look/render")).toBe(true);
  expect(isRenderHash("look/render")).toBe(true);
  expect(isRenderHash("#/look/verdict")).toBe(false);
  expect(isRenderHash("#/")).toBe(false);
});

test("the swipe lands on the original in one tap and never shows a figure", () => {
  expect(clampedPosition(originalPosition)).toBe(originalPosition);
  expect(clampedPosition(renderPosition)).toBe(renderPosition);
  expect(clampedPosition(midPosition)).toBe(midPosition);
  expect(clampedPosition(1000)).toBe(originalPosition);
  expect(clampedPosition(-1000)).toBe(renderPosition);

  const swipe = readFileSync(path.join(here, "swipe.svelte"), "utf8");
  expect(swipe).toContain('type="range"');
  expect(swipe).toContain('aria-label="Swipe between the original and the render"');
  expect(swipe).toContain("bind:value={position}");
  expect(swipe).toContain('src={before}');
  expect(swipe).toContain('src={after}');
  expect(swipe).toContain("position = clampedPosition(originalPosition)");
  // A tap on the original covers the frame completely: nothing of the render peeks out.
  expect(swipe).toContain('style:clip-path="inset(0 {clip}% 0 0)"');
  expect(swipe).toContain("const clip = $derived(originalPosition - clampedPosition(position));");
  // The split is visible: a line at the clip, a handle riding it, and the
  // two corners naming their side. The corners are the taps now — buttons
  // above the range input — so no second row of buttons stands below the
  // arch.
  expect(swipe).toContain('style:left="{clampedPosition(position)}%"');
  expect(swipe).toContain('<span aria-hidden="true">◂ ▸</span>');
  expect(swipe).toContain('<button type="button" class="badge original" onclick={showOriginal}>Original</button>');
  expect(swipe).toContain('<button type="button" class="badge after" onclick={showRender}>After the fix</button>');
  expect(swipe).not.toContain("Show the original");
  expect(swipe).not.toContain("Show the render");
  const styles = swipe.slice(swipe.indexOf("<style>"));
  expect(styles).toMatch(/\.frame\s*\{[^}]*border-radius: 999px 999px var\(--radius-panel\) var\(--radius-panel\)/);
  expect(styles).toMatch(/\.frame\s*\{[^}]*border: 2px solid var\(--line\)/);
  expect(styles).toMatch(/\.frame\s*\{[^}]*max-width: min\(100%, calc\(72dvh \* 3 \/ 4\)\)/);
  expect(styles).toMatch(/\.divider\s*\{[^}]*width: 2px/);
  expect(styles).toMatch(/\.handle\s*\{[^}]*width: 2\.5rem/);
  expect(styles).toMatch(/\.handle\s*\{[^}]*border-radius: 999px/);
  expect(styles).toMatch(/\.badge\s*\{[^}]*min-height: 2\.75rem/);
  expect(styles).toMatch(/\.badge\s*\{[^}]*background: var\(--surface\)/);
  expect(styles).toMatch(/\.badge\s*\{[^}]*border: 1px solid var\(--line\)/);
  expect(styles).toMatch(/\.badge\s*\{[^}]*color: var\(--text\)/);
  expect(styles).toMatch(/\.badge\s*\{[^}]*z-index: 2/);
  // A tap glides; a drag, and any forced-reduction reader, does not wait.
  expect(styles).toMatch(/@media \(prefers-reduced-motion: no-preference\)\s*\{[\s\S]*?transition: clip-path/);
  const text = staticText(swipe);
  expect(text).not.toMatch(/\d/);
  expect(text).toContain("Original");
  expect(text).toContain("After the fix");
  expect(text).not.toContain("Show the original");
  expect(text).not.toContain("Show the render");
});

test("the sheet lists every product with its label", async () => {
  installMock();
  const shop = await loadShop(storyLookId);
  expect(lastFixtureFile).toBe("shop.json");
  expect(shop.products).toHaveLength(1);
  expect(shop.products[0]).toEqual({
    id: "gold-jhumkas",
    name: "Gold jhumkas",
    label: "exact product",
    url: "/media/c0a10600000000000000000000000001",
  });

  // The recorded visual-simulation shape, served at the story's path.
  const simulation = disk("shop-simulation.json");
  pinAnswer("GET", `/api/looks/${storyLookId}/shop`, {
    file: "shop-simulation.json",
    method: "GET",
    path: `/api/looks/${storyLookId}/shop`,
    status: 200,
    body: simulation.body,
  });
  const pinned = await loadShop(storyLookId);
  expect(pinned.products[0]?.label).toBe("visual simulation");
  expect(pinned.products[0]?.url).toBeUndefined();

  // Both recorded products together, each with its own label.
  const catalogue = disk("shop.json");
  const both = {
    products: [
      (catalogue.body as { products: unknown[] }).products[0],
      (simulation.body as { products: unknown[] }).products[0],
    ],
  };
  pinAnswer("GET", `/api/looks/${storyLookId}/shop`, {
    file: "shop-two.json",
    method: "GET",
    path: `/api/looks/${storyLookId}/shop`,
    status: 200,
    body: both,
  });
  const two = await loadShop(storyLookId);
  expect(two.products.map((product) => product.label)).toEqual(["exact product", "visual simulation"]);

  const sheet = readFileSync(path.join(here, "sheet.svelte"), "utf8");
  expect(sheet).toContain("{product.name}");
  expect(sheet).toContain("{product.label}");
  expect(sheet).toContain("{#if product.url}");
  expect(sheet).toContain("{shopSentence}");
  expect(sheet).toContain("Shop the look");
  // The label opens its own plain explanation, one sentence per fidelity word.
  expect(sheet).toContain("<Popover.Root>");
  expect(sheet).toContain('<Popover.Trigger class="fidelity-trigger">');
  expect(sheet).toContain('<span class="label">{product.label}</span>');
  expect(sheet).toContain("{fidelityExplanations[product.label]}");
  expect(sheet).toContain('aria-label="What the label means"');
  const text = staticText(sheet);
  expect(text).not.toMatch(/\d/);
  expect(text).toContain("Shop the look");
  expect(text).not.toContain("lookId");
});

test("keep, dismiss and bought each reach the API client", async () => {
  installMock();
  const inner = globalThis.fetch;
  const calls: { method: string; path: string; body: string }[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const url = new URL(raw, "http://dresscode.local");
    calls.push({
      method: (init?.method ?? "GET").toUpperCase(),
      path: url.pathname,
      body: typeof init?.body === "string" ? init.body : "",
    });
    return inner(input, init);
  }) as typeof fetch;

  for (const verdict of lookVerdicts) {
    const stored = await sendVerdict(storyLookId, verdict);
    expect(stored).toBe(verdict);
  }
  expect(calls).toEqual([
    { method: "PUT", path: `/api/looks/${storyLookId}/verdict`, body: '{"verdict":"kept"}' },
    { method: "PUT", path: `/api/looks/${storyLookId}/verdict`, body: '{"verdict":"dismissed"}' },
    { method: "PUT", path: `/api/looks/${storyLookId}/verdict`, body: '{"verdict":"bought"}' },
  ]);
  // The mock picked each recorded answer by the verdict in the body, so the
  // body above is the one the fixtures record.
  expect(lastFixtureFile).toBe("verdict-bought.json");
});

test("the look with no passing render stands on the suggestion", async () => {
  installMock();
  const copy = await loadLook(suggestionLookId);
  expect(copy).not.toBeNull();
  expect(copy?.render).toBeUndefined();
  expect(copy?.selfieUrl).toBe("/media/5e1f1e00000000000000000000000001");
  expect(copy?.fix).toBe("Adds gold jhumkas.");
  expect(copy?.why).toBe("The ear reads plain; try gold jhumkas.");

  // The suggestion-only look's shop list is recorded too: the products stand.
  const shop = await loadShop(suggestionLookId);
  expect(lastFixtureFile).toBe("shop-none.json");
  expect(shop.products.map((product) => product.label)).toEqual(["exact product"]);

  // A pending look is not a verdict; the screen says it is still being finished.
  preferFixture("GET", `/api/looks/${storyLookId}`, "look-pending.json");
  expect(await loadLook(storyLookId)).toBeNull();

  // Guideline six, at the app boundary: a recorded render whose check did not
  // pass all three is discarded here, whatever carried it.
  const failed = disk("look.json");
  const shown = (failed.body as { shown_render: { check: Record<string, unknown> } }).shown_render;
  shown.check.same_person = false;
  const discarded = renderFromLook(failed.body);
  expect(discarded?.render).toBeUndefined();
  const broken = renderFromLook({
    ...(failed.body as Record<string, unknown>),
    shown_render: { url: "/media/x", check: { change_made: true, same_person: true } },
  });
  expect(broken?.render).toBeUndefined();
});

test("the screen shows the render, the actions and the sheet, and no digit", async () => {
  installMock();
  const copy = await loadLook(storyLookId);
  expect(lastFixtureFile).toBe("look.json");
  expect(copy?.render).toEqual({ url: "/media/2e4de200000000000000000000000001" });
  expect(copy?.reasons).toEqual([
    "The ear reads plain; try gold jhumkas.",
    "The lip reads bare; try a red lip.",
  ]);

  const screen = readFileSync(path.join(here, "render.svelte"), "utf8");
  // The render is only ever mounted inside the branch that has one. Without
  // one, the plain sentence, the original and the sheet stand.
  expect(screen).toContain("{#if look.render}");
  expect(screen).toContain("<Swipe before={look.selfieUrl} after={look.render.url} />");
  expect(screen).toContain("{suggestionSentence}");
  expect(screen).toContain('src={look.selfieUrl}');
  expect(screen).toContain("<ShopSheet lookId={look.lookId} />");
  expect(screen).toContain("{verdictWords[choice]}");
  expect(screen).toContain("{savedSentences[saved]}");
  expect(screen).toContain("{#each lookVerdicts as choice (choice)}");
  expect(screen).toContain("{pendingSentence}");
  // The fix is the hero; the verdict actions sit in equal segments, the
  // stored one (Keep, until a choice is stored) carrying the primary fill.
  expect(screen).toContain('<div class="hero">');
  expect(screen).toContain('<h3 class="hero-label">A fix</h3>');
  expect(screen).toContain('<p class="hero-fix">{look.fix}</p>');
  // The why sentence repeated reasons[0] word for word; it is gone, and the
  // fix paragraph is the card's only body.
  expect(screen).not.toContain("{look.why}");
  expect(screen).not.toContain(">Why<");
  expect(screen).toContain('class:primary={(saved ?? "kept") === choice}');
  // The missed criteria fold away: the closed drawer carries them, named by
  // its summary.
  expect(screen).toContain('<details class="disclosure reasons-drawer">');
  expect(screen).toContain("<summary>What the dress code still asks for</summary>");
  expect(screen).toContain("{reason}");
  const styles = screen.slice(screen.indexOf("<style>"));
  expect(styles).toMatch(/\.hero\s*\{[^}]*background: var\(--raised\)/);
  expect(styles).toMatch(/\.hero\s*\{[^}]*border-left: 3px solid var\(--accent\)/);
  expect(styles).toMatch(/\.verdict\s*\{[^}]*grid-template-columns: repeat\(3, minmax\(0, 1fr\)\)/);
  const text = staticText(screen);
  expect(text).not.toMatch(/\d/);
  expect(text).toContain("The render");
  expect(text).toContain("What the dress code still asks for");
  expect(text).toContain("A fix");
  expect(text).not.toContain("Why");
  expect(text).not.toContain("lookId");
  expect(text).not.toContain("jobId");

  // A failure is a sentence, not the wire phrase.
  expect(humanMessage(shownFailure(new Error("nope")))).toBe("The render could not be read.");
});

test("each label survives a 375 px frame in the styles the gate can read", () => {
  const sheet = readFileSync(path.join(here, "sheet.svelte"), "utf8");
  const render = readFileSync(path.join(here, "render.svelte"), "utf8");
  const swipe = readFileSync(path.join(here, "swipe.svelte"), "utf8");

  // The sheet spans the phone, so a label never sits on a wider canvas.
  expect(styleBlock(sheet)).toContain("width: min(40rem, 100%)");
  // A long name or label wraps inside its row instead of pushing the frame wide.
  expect(styleBlock(sheet)).toMatch(/\.name\s*\{[^}]*overflow-wrap: break-word/);
  expect(styleBlock(sheet)).toMatch(/\.label\s*\{[^}]*overflow-wrap: break-word/);
  expect(styleBlock(sheet)).toMatch(/\.product\s*\{[^}]*min-inline-size: 0/);
  // The frames take the column they are given, never a fixed width.
  expect(styleBlock(swipe)).toMatch(/\.swipe\s*\{[^}]*min-inline-size: 0/);
  expect(styleBlock(swipe)).toMatch(/\.frame\s*\{[^}]*width: 100%/);
  expect(styleBlock(render)).toMatch(/\.render\s*\{[^}]*min-inline-size: 0/);
  // And nothing pins a line against wrapping.
  for (const styles of [styleBlock(sheet), styleBlock(render), styleBlock(swipe)]) {
    expect(styles).not.toContain("white-space: nowrap");
  }

  const shell = readFileSync(path.join(here, "../../../app/app.svelte"), "utf8");
  // The render is a hash branch of the app, not a footer text link: the blog
  // row that read `See the render` is gone with the tab-bar shell.
  expect(shell).toContain("isRenderHash");
  expect(shell).toContain('route === "/look/render"');
  expect(shell).toContain("<Render />");
  const shellText = staticText(shell);
  expect(shellText).not.toMatch(/\d/);
  expect(shellText).not.toContain("See the render");
});

test("a shop list that cannot be read throws before the sheet shows it", () => {
  expect(() => shopFromResponse({})).toThrow();
  expect(() => shopFromResponse({ products: [{ id: "x", name: "X", fidelity: "exact" }] })).toThrow();
  expect(() => shopFromResponse({ products: [{ id: "x", name: "X", fidelity: "exact product" }] })).not.toThrow();
  expect(() => renderFromLook(null)).toThrow();
  expect(() => renderFromLook({ id: "x", ready: true })).toThrow();
});
