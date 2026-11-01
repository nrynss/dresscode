import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";

/** The sample chips' faces, checked against the recordings they stand on.
 * Each of the five sample stories names its own person (the `selfie` member
 * of its recorded file under `testdata/samples/cached/`), the front door's
 * chip table maps each sample id to a media path, and the sample-media map in
 * `web/vite.config.ts` serves each path the recorded person's photograph.
 * Three reads, one truth: a chip that shows another sample's face fails
 * here, and so does a map entry that points a served id at the wrong file.
 * jsdom cannot judge a photograph (and no assertion reads image bytes), so
 * this guard pins the mapping, and the browser rig pins the drawn chips. */

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../..");
const repoRoot = path.resolve(here, "../../..");

/** The sample-media map, read out of `web/vite.config.ts` — the one source
 * the dev middleware and the mock's virtual module both read, so the served
 * pin reads the same table they do. */
function mediaSamples(): Map<string, string> {
  const source = readFileSync(path.join(webRoot, "vite.config.ts"), "utf8");
  const map = new Map<string, string>();
  for (const match of source.matchAll(/"(\/media\/[0-9a-f]+)":\s*"([^"]+)"/g)) {
    map.set(match[1]!, match[2]!);
  }
  return map;
}

/** The front door's chip table, read out of `web/src/app/app.svelte`. */
function chipFaces(): Map<string, string> {
  const source = readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8");
  const map = new Map<string, string>();
  for (const match of source.matchAll(/"(sample-[a-z-]+)":\s*"(\/media\/[0-9a-f]+)"/g)) {
    map.set(match[1]!, match[2]!);
  }
  return map;
}

/** The recorded sample stories: the ground truth for whose face a chip is
 * of. The server's own loader reads these files at mount and refuses a story
 * whose selfie is not its variant's picture, so they are evidence, not a
 * wish list. */
interface RecordedSample {
  id: string;
  variant: string;
  selfie: string;
}

function recordedSamples(): RecordedSample[] {
  const dir = path.join(repoRoot, "testdata", "samples", "cached");
  const stories: RecordedSample[] = [];
  for (const name of readdirSync(dir)) {
    if (!name.startsWith("sample-") || !name.endsWith(".json")) continue;
    const story = JSON.parse(readFileSync(path.join(dir, name), "utf8")) as Record<string, unknown>;
    stories.push({
      id: story.id as string,
      variant: story.variant as string,
      selfie: story.selfie as string,
    });
  }
  return stories.sort((a, b) => a.id.localeCompare(b.id));
}

const faces = mediaSamples();
const chips = chipFaces();
const stories = recordedSamples();

test("every recorded sample's chip shows its own recorded person", () => {
  expect(stories.length, "the five recorded sample stories are read").toBe(5);
  expect(
    [...chips.keys()].sort(),
    "the chip table covers exactly the recorded samples",
  ).toEqual(stories.map((story) => story.id).sort());
  for (const story of stories) {
    const chip = chips.get(story.id);
    expect(chip, `${story.id} names a face`).toBeDefined();
    const served = faces.get(chip!);
    expect(served, `${story.id}'s face is one the media map serves`).toBeDefined();
    expect(
      path.resolve(webRoot, served!),
      `${story.id} (${story.variant}) shows its own recorded person, ${story.selfie}`,
    ).toBe(path.resolve(repoRoot, "testdata", "samples", ...story.selfie.split("/")));
  }
});

test("the five chips name five distinct faces", () => {
  expect(new Set(chips.values()).size, "no two chips share one face").toBe(chips.size);
});

test("no media path the app names goes unserved", () => {
  const app = readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8");
  const named = [...app.matchAll(/\/media\/[0-9a-f]+/g)].map((match) => match[0]);
  expect(named.length, "the app names served media paths").toBeGreaterThan(0);
  for (const mediaPath of new Set(named)) {
    expect([...faces.keys()], `${mediaPath} is served`).toContain(mediaPath);
  }
});

test("every file the media map names exists", () => {
  expect(faces.size, "the media map is read").toBeGreaterThan(0);
  for (const [mediaPath, file] of faces) {
    expect(() => readFileSync(path.resolve(webRoot, file)), `${mediaPath} serves a real file`).not.toThrow();
  }
});
