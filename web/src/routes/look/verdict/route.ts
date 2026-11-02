/** Hash route. The app is a static page, so this screen is not a server path. */
export const verdictHash = "#/look/verdict";

/** The occasion screen's hash, where the fix menus and their ticks live. The
 * shell routes the hash by its own literal; the verdict's way forward binds
 * this constant, so a swap on either side fails a test instead of a user. */
export const occasionHash = "#/occasion";

export function isVerdictHash(hash: string): boolean {
  return hash === verdictHash || hash === "/look/verdict" || hash === "look/verdict";
}
