import { loadFixtures } from "./fixtures";
import type { ApiFixture } from "./types";

/** The family-function story when more than one file records the same path.
 * History is off until the user turns it on, so recall is the off fixture.
 * The finished look is the one the shell can open. A verify or verdict body
 * still picks its own file. */
const storyFile: Record<string, string> = {
  "GET /api/looks/100c0000000000000000000000000001": "look.json",
  "GET /api/history/recall?occasion=family-function": "recall-off.json",
  "POST /api/session/code/verify": "session-verify.json",
  "PUT /api/looks/100c0000000000000000000000000001/verdict": "verdict.json",
};

const pins = new Map<string, ApiFixture>();
const preferences = new Map<string, string>();

let restoreFetch: (() => void) | undefined;

/** Credentials and fixture file of the latest mocked call. Tests read these. */
export let lastCredentials: RequestCredentials | undefined;
export let lastFixtureFile: string | undefined;

function key(method: string, path: string): string {
  return `${method.toUpperCase()} ${path}`;
}

function requestPath(raw: string): string {
  try {
    const url = new URL(raw, "http://dresscode.local");
    return url.pathname + url.search;
  } catch {
    return raw;
  }
}

function readJson(body: string): Record<string, unknown> | undefined {
  if (body === "") return undefined;
  try {
    const value: unknown = JSON.parse(body);
    if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
    return value as Record<string, unknown>;
  } catch {
    return undefined;
  }
}

function byFile(matches: ApiFixture[], file: string): ApiFixture | undefined {
  return matches.find((item) => item.file === file || item.file.endsWith(`/${file}`));
}

function choose(matches: ApiFixture[], method: string, path: string, body: string): ApiFixture | undefined {
  if (matches.length === 0) return undefined;
  if (matches.length === 1) return matches[0];
  const preferred = preferences.get(key(method, path));
  if (preferred) {
    const chosen = byFile(matches, preferred);
    if (chosen) return chosen;
  }
  if (method === "POST" && path === "/api/session/code/verify") {
    const choice = readJson(body)?.choice;
    const file = choice === "switch" ? "session-verify-switch.json" : "session-verify.json";
    return byFile(matches, file) ?? matches[0];
  }
  if (method === "PUT" && path.endsWith("/verdict")) {
    const verdict = readJson(body)?.verdict;
    const file =
      verdict === "bought" ? "verdict-bought.json" : verdict === "dismissed" ? "verdict-dismissed.json" : "verdict.json";
    return byFile(matches, file) ?? matches[0];
  }
  const story = storyFile[key(method, path)];
  if (story) return byFile(matches, story) ?? matches[0];
  return matches[0];
}

function resolve(catalog: ApiFixture[], method: string, path: string, body: string): ApiFixture | undefined {
  const pinned = pins.get(key(method, path));
  if (pinned) return pinned;
  const matches = catalog.filter((item) => item.method === method && item.path === path);
  return choose(matches, method, path, body);
}

function fixtureResponse(fixture: ApiFixture): Response {
  const headers = new Headers();
  if (fixture.headers) {
    for (const [name, value] of Object.entries(fixture.headers)) headers.set(name, value);
  }
  let payload: BodyInit | null = null;
  if (typeof fixture.body === "string") payload = fixture.body;
  else if (fixture.body !== null && fixture.body !== undefined) {
    payload = `${JSON.stringify(fixture.body)}\n`;
    if (!headers.has("content-type")) headers.set("content-type", "application/json");
  }
  return new Response(payload, { status: fixture.status ?? 200, headers });
}

/** Serve one recorded file for a path, whatever the story default is. */
export function preferFixture(method: string, path: string, file: string): void {
  preferences.set(key(method, path), file);
}

/** Serve an envelope that has no path of its own, or any other fixture, at a path. */
export function pinAnswer(method: string, path: string, fixture: ApiFixture): void {
  pins.set(key(method, path), fixture);
}

export function resetMockChoices(): void {
  pins.clear();
  preferences.clear();
}

/** Install the fixture fetch. Calling it again returns the same uninstall. */
export function installMock(): () => void {
  if (restoreFetch) return restoreFetch;
  const previous = globalThis.fetch.bind(globalThis);
  const catalog = loadFixtures();
  const notFound = catalog.find((item) => item.file === "errors/not-found.json");
  const mediaMissing = catalog.find((item) => item.file === "media-missing.json");

  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    const raw = input instanceof Request ? input.url : input instanceof URL ? input.href : String(input);
    const path = requestPath(raw);
    lastCredentials = init?.credentials ?? (input instanceof Request ? input.credentials : undefined);
    if (!path.startsWith("/api/") && !path.startsWith("/media/")) return previous(input, init);

    let body = "";
    if (typeof init?.body === "string") body = init.body;
    else if (input instanceof Request) body = await input.clone().text();

    const hit = resolve(catalog, method, path, body);
    const chosen = hit ?? (path.startsWith("/media/") ? mediaMissing : notFound);
    if (chosen) {
      lastFixtureFile = chosen.file;
      return fixtureResponse(chosen);
    }
    lastFixtureFile = undefined;
    return new Response("not found\n", { status: 404 });
  }) as typeof fetch;

  restoreFetch = () => {
    globalThis.fetch = previous;
    restoreFetch = undefined;
    resetMockChoices();
    lastCredentials = undefined;
    lastFixtureFile = undefined;
  };
  return restoreFetch;
}
