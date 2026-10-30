import { api as keelApi, type ApiRequestInit } from "@nrynss/chaaya/keel";

export { ApiError, keelErrorParser, parseErrorEnvelope, parseJobEvent } from "@nrynss/chaaya/keel";

/** Dresscode's JSON client. It is Chaaya's Keel client, so a wire envelope
 * arrives as ApiError with the stable code. Cookies go with the call.
 * There is no bearer token. Chunk uploads and event streams use Chaaya's
 * own upload and job clients; this one speaks JSON. */
export function api<T>(path: string, init: ApiRequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (!headers.has("accept")) headers.set("accept", "application/json");
  if (typeof init.body === "string" && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }
  return keelApi<T>(path, {
    ...init,
    credentials: init.credentials ?? "include",
    headers,
  });
}
