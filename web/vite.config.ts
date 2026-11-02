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

/** The dev-server surface the media middleware needs; the project's
 * typecheck carries no Node types package. */
interface DevServer {
  middlewares: {
    use(
      handler: (
        req: { method?: string; url?: string },
        res: {
          setHeader(name: string, value: string): void;
          end(chunk: string, encoding?: string): void;
        },
        next: () => void,
      ) => void,
    ): void;
  };
}

/** The recorded media ids the story screens show, and the committed sample
 * photograph each one serves in mock mode.
 *
 * The `5e1f1e` ids are sample people's faces, one per sample chip on the
 * front door: each names the selfie the sample's own recorded story carries
 * (the `selfie` member of `testdata/samples/cached/sample-<id>.json`), so a
 * chip shows the person its story is about — interview and funeral are men,
 * the other three the woman. The family id doubles as the recorded look
 * body's selfie (`testdata/api/look.json` names it). `2e4de2` is that look's
 * shown render and the two `c0a1` ids the recorded product photo; the
 * recorded bodies name those, and the chips do not. */
const mediaSamples: Record<string, string> = {
  "/media/5e1f1e00000000000000000000000001": "../testdata/samples/woman/V04-family-function.jpg",
  "/media/5e1f1e00000000000000000000000002": "../testdata/samples/woman/V03-date.jpg",
  "/media/5e1f1e00000000000000000000000003": "../testdata/samples/man/V09-interview.jpg",
  "/media/5e1f1e00000000000000000000000004": "../testdata/samples/woman/V14-festival.jpg",
  "/media/5e1f1e00000000000000000000000005": "../testdata/samples/man/V18-funeral.jpg",
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
    /** An `<img>` load bypasses the fetch interception the mock installs, so
     * the dev server itself answers the recorded ids from the same id-to-file
     * map the virtual module embeds — and the samples mount the chips'
     * avatars name, in dev as in production. */
    configureServer(server: DevServer) {
      server.middlewares.use((req, res, next) => {
        const url = req.url ?? "";
        if (req.method !== "GET") {
          next();
          return;
        }
        if (url.startsWith("/samples/") && url.endsWith(".jpg")) {
          const file = path.resolve(root, "../testdata/samples", decodeURIComponent(url.slice("/samples/".length).split("?")[0]));
          if (!file.startsWith(path.resolve(root, "../testdata/samples"))) {
            next();
            return;
          }
          res.setHeader("Content-Type", "image/jpeg");
          res.end(fs.readFileSync(file).toString("base64"), "base64");
          return;
        }
        if (!url.startsWith("/media/")) {
          next();
          return;
        }
        const sample = mediaSamples[new URL(url, "http://dresscode.local").pathname];
        if (sample === undefined) {
          next();
          return;
        }
        res.setHeader("Content-Type", "image/jpeg");
        res.end(fs.readFileSync(path.resolve(root, sample)).toString("base64"), "base64");
      });
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
