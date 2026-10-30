import { svelte } from "@sveltejs/vite-plugin-svelte";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const root = path.dirname(fileURLToPath(import.meta.url));
const fixtureModule = "virtual:dresscode-api-fixtures";
const fixtureResolved = "\0virtual:dresscode-api-fixtures";

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

export default defineConfig({
  plugins: [svelte(), dresscodeFixtures()],
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
