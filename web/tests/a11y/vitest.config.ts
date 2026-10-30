import base from "../../vite.config.ts";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "../..");
const repoRoot = path.resolve(here, "../../..");

/** The accessibility gates. The app's own config brings the Svelte plugin and
 * the fixture module; this file points Vitest at the gates' directory, gives
 * them the DOM the accessibility gate reads, and lets the module loader serve
 * `product.md?raw`, which the account screen reads from the repository root. */
export default defineConfig({
  ...base,
  /** Vitest resolves the modules a test imports through the server pipeline,
   * so the browser condition is what keeps Svelte's client runtime — the one
   * `mount` needs — ahead of the server build. */
  resolve: {
    ...base.resolve,
    conditions: [...(base.resolve?.conditions ?? []), "browser"],
  },
  server: {
    ...base.server,
    fs: {
      ...base.server?.fs,
      allow: [...(base.server?.fs?.allow ?? []), webRoot, repoRoot],
    },
  },
  test: {
    ...base.test,
    environment: "jsdom",
    include: ["tests/a11y/**/*.test.ts"],
    setupFiles: ["./tests/a11y/setup.ts"],
  },
});
