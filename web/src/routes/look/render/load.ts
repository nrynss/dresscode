import { ApiError, api } from "../../../lib/api/client";
import type { ShownError } from "../../../lib/api/types";
import {
  isLookVerdict,
  renderFromLook,
  shopFromResponse,
  type LookVerdict,
  type RenderCopy,
  type ShopList,
} from "./copy";

/** The recorded family-function look the screen follows. The ids are not shown. */
export const storyLookId = "100c0000000000000000000000000001";

/** The ready look, or null while the job is still running. */
export async function loadLook(lookId: string, signal?: AbortSignal): Promise<RenderCopy | null> {
  return renderFromLook(await api<unknown>(`/api/looks/${lookId}`, { signal }));
}

/** Shop the look. Every product arrives with its fidelity label. */
export async function loadShop(lookId: string, signal?: AbortSignal): Promise<ShopList> {
  return shopFromResponse(await api<unknown>(`/api/looks/${lookId}/shop`, { signal }));
}

/** Keep, dismiss, bought. One field, one stored value. The stored verdict comes back. */
export async function sendVerdict(lookId: string, verdict: LookVerdict, signal?: AbortSignal): Promise<LookVerdict> {
  const body = await api<unknown>(`/api/looks/${lookId}/verdict`, {
    method: "PUT",
    body: JSON.stringify({ verdict }),
    signal,
  });
  const stored = typeof body === "object" && body !== null ? (body as Record<string, unknown>).verdict : undefined;
  if (typeof stored !== "string" || !isLookVerdict(stored)) throw new Error("The verdict could not be saved.");
  return stored;
}

/** A sentence the screen can show. The wire phrase is not passed through. */
export function shownFailure(error: unknown): ShownError {
  if (error instanceof ApiError) return { code: error.code };
  return { code: "render", message: "The render could not be read." };
}
