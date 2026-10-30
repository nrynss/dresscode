import { svelte } from "@sveltejs/vite-plugin-svelte";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const root = path.dirname(fileURLToPath(import.meta.url));
const fixtureModule = "virtual:dresscode-api-fixtures";
const fixtureResolved = "\0virtual:dresscode-api-fixtures";
const mediaModule = "virtual:dresscode-sample-media";
const mediaResolved = "\0virtual:dresscode-sample-media";

/** The recorded media ids the story screens show, and the committed sample
 * photograph each one serves in mock mode. */
const mediaSamples: Record<string, string> = {
  "/media/5e1f1e00000000000000000000000001": "../testdata/samples/woman/V04-family-function.jpg",
  "/media/2e4de200000000000000000000000001": "../testdata/samples/woman/V05-family-function.jpg",
  "/media/c0a10600000000000000000000000001": "../testdata/samples/products/gold-jhumkas.jpg",
  "/media/c0a10cf0000000000000000000000001": "../testdata/samples/products/gold-jhumkas.jpg",
};

/** Embed testdata/api so mock mode can serve it from the static app. */
function dresscodeFixtures() {
  return {
    name: "dresscode-api-fixtures",
    resolveId(id: string) {
      if (id === fixtureModule) return fixtureResolved;
      return null;
    },
    load(id: string) {
      if (id !== fixtureResolved) return null;
      const dir = path.resolve(root, "../testdata/api");
      const files: { file: string; json: unknown }[] = [];
      const walk = (abs: string, rel: string) => {
        for (const name of fs.readdirSync(abs)) {
          const child = path.join(abs, name);
          const next = rel ? `${rel}/${name}` : name;
          if (fs.statSync(child).isDirectory()) walk(child, next);
          else if (name.endsWith(".json")) {
            files.push({ file: next, json: JSON.parse(fs.readFileSync(child, "utf8")) as unknown });
          }
        }
      };
      walk(dir, "");
      files.sort((a, b) => a.file.localeCompare(b.file));
      return `export default ${JSON.stringify(files)};`;
    },
  };
}

/** Embed the sample JPEGs so mock mode's /media/{id} serves a photograph. */
function dresscodeSampleMedia() {
  return {
    name: "dresscode-sample-media",
    resolveId(id: string) {
      if (id === mediaModule) return mediaResolved;
      return null;
    },
    load(id: string) {
      if (id !== mediaResolved) return null;
      const media: Record<string, string> = {};
      for (const [mediaPath, rel] of Object.entries(mediaSamples)) {
        media[mediaPath] = fs.readFileSync(path.resolve(root, rel)).toString("base64");
      }
      return `export default ${JSON.stringify(media)};`;
    },
  };
}

export default defineConfig({
  plugins: [svelte(), dresscodeFixtures(), dresscodeSampleMedia()],
  publicDir: "static",
  build: {
    outDir: "build",
    emptyOutDir: true,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
