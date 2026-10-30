/** jsdom carries no media pipeline. The capture screen's preview element is a
 * real <video>, and jsdom only warns on its play and pause calls, so they
 * become the no-ops they already are here. */
if (typeof HTMLMediaElement !== "undefined") {
  for (const method of ["play", "pause", "load"] as const) {
    (HTMLMediaElement.prototype as Record<string, unknown>)[method] = () => {};
  }
}

/** Two browser behaviours jsdom does not carry: layout observers and programmatic
 * scroll-into-view. The gates never read layout (Chaaya's accessibility gate
 * disables the layout rules itself), but the dialogs the screens open touch both. */
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = ResizeObserverStub;
}

if (typeof Element !== "undefined" && typeof Element.prototype.scrollIntoView !== "function") {
  (Element.prototype as { scrollIntoView?: () => void }).scrollIntoView = function scrollIntoView(): void {};
}

/** jsdom carries no matchMedia. The app's theme control reads the system
 * preference through it, so the stub answers "light system" and accepts the
 * change listener, which nothing fires here. */
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}
