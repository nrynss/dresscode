/** Hash route. The app is a static page, so this screen is not a server path. */
export const historyHash = "#/history";

export function isHistoryHash(hash: string): boolean {
  return hash === historyHash || hash === "/history" || hash === "history";
}
