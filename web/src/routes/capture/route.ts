/** Hash route. The app is a static page, so this screen is not a server path. */
export const captureHash = "#/capture";

export function isCaptureHash(hash: string): boolean {
  return hash === captureHash;
}
