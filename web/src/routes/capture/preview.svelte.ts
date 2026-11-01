/**
 * The object URL behind the capture screen's mirror preview. A photo shown
 * inside the arch is a `blob:` URL over the prepared bytes, and a `blob:` URL
 * pins those bytes in memory, so the holder is responsible for handing each
 * one back: pointing the preview at new bytes revokes the old URL, and
 * releasing the preview revokes the current one.
 */
export class PhotoPreview {
  #url = $state("");

  /** The current object URL, or "" when the mirror has no photo to show. */
  get url(): string {
    return this.#url;
  }

  /** Point the preview at freshly prepared bytes. The previous photo's URL is
   * revoked first, so a replacement never leaves the old bytes pinned. */
  replace(blob: Blob): void {
    this.release();
    this.#url = URL.createObjectURL(blob);
  }

  /** Revoke the URL it currently holds, if it holds one. Safe to call twice. */
  release(): void {
    if (this.#url !== "") {
      URL.revokeObjectURL(this.#url);
      this.#url = "";
    }
  }
}
