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
