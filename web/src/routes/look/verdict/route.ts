/** Hash route. The app is a static page, so this screen is not a server path. */
export const verdictHash = "#/look/verdict";

export function isVerdictHash(hash: string): boolean {
  return hash === verdictHash || hash === "/look/verdict" || hash === "look/verdict";
}
