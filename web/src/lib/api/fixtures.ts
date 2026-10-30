import raw from "virtual:dresscode-api-fixtures";
import type { ApiFixture } from "./types";

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function asStringRecord(value: unknown): Record<string, string> | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  const headers: Record<string, string> = {};
  for (const [name, header] of Object.entries(record)) {
    if (typeof header === "string") headers[name] = header;
  }
  return headers;
}

/** Every file under testdata/api, including the error envelopes. */
export function loadFixtures(): ApiFixture[] {
  return raw.map((entry) => {
    const json = asRecord(entry.json);
    if (!json) throw new Error(`fixture ${entry.file} is not an object`);
    return {
      file: entry.file,
      method: typeof json.method === "string" ? json.method : undefined,
      path: typeof json.path === "string" ? json.path : undefined,
      status: typeof json.status === "number" ? json.status : undefined,
      headers: asStringRecord(json.headers),
      body: json.body,
      where: typeof json.where === "string" ? json.where : undefined,
    };
  });
}

/** The code on a recorded error envelope, when the file has one. */
export function fixtureCode(fixture: ApiFixture): string | undefined {
  const body = asRecord(fixture.body);
  const error = body ? asRecord(body.error) : undefined;
  return error && typeof error.code === "string" ? error.code : undefined;
}
