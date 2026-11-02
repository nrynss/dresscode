import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";

/** The sample chips' faces, checked against the recordings they stand on.
 * Each of the five sample stories names its own person (the `selfie` member
 * of its recorded file under `testdata/samples/cached/`), the front door's
 * chip table maps each sample id to that path under the samples mount
 * (`/samples/<selfie>`, which the server publishes in production and the
 * dev middleware answers here). Three reads, one truth: a chip that shows
 * another sample's face fails here. jsdom cannot judge a photograph (and no
 * assertion reads image bytes), so this guard pins the mapping, and the
 * browser rig pins the drawn chips. */

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../..");
const repoRoot = path.resolve(here, "../../..");

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

/** The faces the samples mount serves: each story's own selfie member, at
 * the path the mount publishes. One source — the stories themselves — so a
 * second table cannot drift from what the server answers. */
function servedFaces(): Map<string, string> {
  const map = new Map<string, string>();
  for (const story of recordedSamples()) {
    map.set(
      `/samples/${story.selfie}`,
      path.join(repoRoot, "testdata", "samples", ...story.selfie.split("/")),
    );
  }
  return map;
}

/** The front door's chip table, read out of `web/src/app/app.svelte`. */
function chipFaces(): Map<string, string> {
  const source = readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8");
  const map = new Map<string, string>();
  for (const match of source.matchAll(/"(sample-[a-z-]+)":\s*"(\/samples\/[^"]+)"/g)) {
    map.set(match[1]!, match[2]!);
  }
  return map;
}

const faces = servedFaces();
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
    expect(served, `${story.id}'s face is one the samples mount serves`).toBeDefined();
    expect(
      path.resolve(served!),
      `${story.id} (${story.variant}) shows its own recorded person, ${story.selfie}`,
    ).toBe(path.resolve(repoRoot, "testdata", "samples", ...story.selfie.split("/")));
  }
});

test("the five chips name five distinct faces", () => {
  expect(new Set(chips.values()).size, "no two chips share one face").toBe(chips.size);
});

test("no samples path the app names goes unserved", () => {
  const app = readFileSync(path.join(webRoot, "src/app/app.svelte"), "utf8");
  const named = [...app.matchAll(/\/samples\/[A-Za-z0-9/.-]+\.jpg/g)].map((match) => match[0]);
  expect(named.length, "the app names served sample photos").toBeGreaterThan(0);
  for (const samplesPath of new Set(named)) {
    const rel = samplesPath.replace("/samples/", "");
    expect(
      () => readFileSync(path.join(repoRoot, "testdata", "samples", ...rel.split("/"))),
      `${samplesPath} is served`,
    ).not.toThrow();
  }
});

test("every file the served faces name exists", () => {
  expect(faces.size, "the served faces are read").toBeGreaterThan(0);
  for (const [, file] of faces) {
    expect(() => readFileSync(file), "the served face is a real file").not.toThrow();
  }
});
