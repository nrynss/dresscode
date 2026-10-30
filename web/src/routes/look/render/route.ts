/** Hash route. The app is a static page, so this screen is not a server path. */
export const renderHash = "#/look/render";

export function isRenderHash(hash: string): boolean {
  return hash === renderHash || hash === "/look/render" || hash === "look/render";
}
