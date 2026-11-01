<script lang="ts">
  import { theme } from "@nrynss/chaaya/theme";
  import { Dialog } from "bits-ui";
  import type { Snippet } from "svelte";
  import Account from "../routes/account/account.svelte";
  import Capture from "../routes/capture/capture.svelte";
  import { captureHash, isCaptureHash } from "../routes/capture/route";
  import { historyHash, isHistoryHash } from "../routes/history/route";
  import { isRenderHash } from "../routes/look/render/route";
  import { isVerdictHash } from "../routes/look/verdict/route";

  let { children }: { children: Snippet } = $props();
  let route = $state(currentHash());

  function currentHash(): string {
    if (typeof window === "undefined") return "";
    return window.location.hash;
  }

  /** The front door and the bare document. Every other hash is a sub-route,
   * and a sub-route is where the back button stands. */
  function isRootHash(hash: string): boolean {
    return hash === "" || hash === "#/" || hash === "#";
  }

  /** The route a hash lands on, normalized exactly the way app.svelte's
   * currentRoute() normalizes it: the leading `#` comes off, and the bare
   * spellings (`history`, `occasion`, `look/verdict`, `look/render`, `home`)
   * land on the same route their slashed form does. The children render from
   * this route, so the tab has to classify it, not the raw hash — a raw-hash
   * check lit no tab at all for `#history` while the History screen was on
   * the page. */
  function normalizedRoute(hash: string): string {
    const bare = hash.replace(/^#/, "");
    if (bare === "/occasion" || bare === "occasion") return "/occasion";
    if (isVerdictHash(bare)) return "/look/verdict";
    if (isRenderHash(bare)) return "/look/render";
    if (isHistoryHash(bare)) return "/history";
    return "/";
  }

  /** The tab a route belongs to. The two screens the shell draws itself are
   * read off the raw hash; everything else is read off the same normalized
   * route the children render from, so the lit tab is always the screen that
   * is actually on the page. The two look screens sit past the four, so they
   * name no tab: their way back is the button, not a tab. */
  function tabOf(hash: string): string {
    if (isCaptureHash(hash)) return "mirror";
    if (hash === "#account") return "account";
    switch (normalizedRoute(hash)) {
      case "/history":
        return "history";
      case "/occasion":
      case "/":
        return "occasion";
      default:
        return "";
    }
  }

  let root = $derived(isRootHash(route));
  let tab = $derived(tabOf(route));

  $effect(() => {
    if (typeof window === "undefined") return;
    const read = () => {
      route = window.location.hash;
    };
    window.addEventListener("hashchange", read);
    return () => window.removeEventListener("hashchange", read);
  });

  $effect(() => {
    const resolved = theme.resolved;
    if (typeof document === "undefined") return;
    if (resolved !== "light" && resolved !== "dark") return;
    const ground = getComputedStyle(document.documentElement).getPropertyValue("--ground").trim();
    if (ground === "") return;
    for (const meta of document.querySelectorAll('meta[name="theme-color"]')) {
      meta.setAttribute("content", ground);
    }
  });
</script>

<div class="shell">
  <header class="bar">
    <div class="bar-side">
      {#if !root}
        <a class="icon-back" href="#/" aria-label="Back">
          <svg
            aria-hidden="true"
            viewBox="0 0 24 24"
            width="20"
            height="20"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M15 18l-6-6 6-6" />
          </svg>
        </a>
      {/if}
      <a class="brand" href="#/">
        <svg viewBox="0 0 32 32" width="28" height="28" fill="none" aria-hidden="true">
          <path
            d="M8 14a8 8 0 0 1 16 0v9a2 2 0 0 1-2 2H10a2 2 0 0 1-2-2v-9Z"
            fill="var(--accent-soft)"
            stroke="var(--accent)"
            stroke-width="2"
          />
          <path d="M12 11l3-3M11 16l6-6" stroke="var(--accent)" stroke-width="1.5" stroke-linecap="round" />
          <path
            d="M19.5 11.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8.8-2.2Z"
            fill="var(--accent)"
          />
          <path d="M16 25v3M11 28h10" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" />
        </svg>
        <span class="wordmark">Dresscode</span>
      </a>
    </div>
    <div class="bar-side">
      <Dialog.Root>
        <Dialog.Trigger class="ghost">About</Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Overlay class="dresscode-overlay" />
          <Dialog.Content class="dresscode-panel">
            <Dialog.Title class="dresscode-title">Dresscode</Dialog.Title>
            <Dialog.Description class="dresscode-copy">
              A private look at the dress code, before anyone else sees the photo. Dresscode suggests a fix. It does
              not grade the photo.
            </Dialog.Description>
            <Dialog.Close class="primary">Close</Dialog.Close>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  </header>
  <main>
    {#if isCaptureHash(route)}
      <Capture />
    {:else if route === "#account"}
      <Account />
    {:else}
      {@render children()}
    {/if}
  </main>
</div>

<nav class="tab-bar" aria-label="Main">
  <div class="tab-grid">
    <a class="tab" href="#/" aria-current={tab === "occasion" ? "page" : undefined}>
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        width="20"
        height="20"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M5 20v-7a7 7 0 0 1 14 0v7" />
        <path d="M3 20h18" />
        <path d="M18.5 3.5l.6 1.6 1.6.6-1.6.6-.6 1.6-.6-1.6-1.6-.6 1.6-.6z" />
      </svg>
      <span>Occasion</span>
    </a>
    <a class="tab" href={captureHash} aria-current={tab === "mirror" ? "page" : undefined}>
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        width="20"
        height="20"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <rect x="6" y="3" width="12" height="13" rx="6" />
        <path d="M12 16v5" />
        <path d="M8.5 21h7" />
      </svg>
      <span>Mirror</span>
    </a>
    <a class="tab" href={historyHash} aria-current={tab === "history" ? "page" : undefined}>
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        width="20"
        height="20"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path d="M3.5 12a8.5 8.5 0 1 0 2.6-6.1" />
        <path d="M3 4v4h4" />
        <path d="M12 8v4.5l3 1.8" />
      </svg>
      <span>History</span>
    </a>
    <a class="tab" href="#account" aria-current={tab === "account" ? "page" : undefined}>
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        width="20"
        height="20"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <circle cx="12" cy="12" r="9" />
        <circle cx="12" cy="10" r="3" />
        <path d="M7 18.6a5.4 5.4 0 0 1 10 0" />
      </svg>
      <span>Account</span>
    </a>
  </div>
</nav>

<style>
  /* The top app bar: the way back on a sub-route, the wordmark, and the
   * About dialog at the far end. */
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.25rem 0.65rem;
    min-inline-size: 0;
  }

  .bar-side {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    min-inline-size: 0;
  }

  .icon-back {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 2.75rem;
    height: 2.75rem;
    border-radius: 999px;
    background: var(--surface);
    border: 1px solid var(--line);
    color: var(--text);
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    min-inline-size: 0;
    color: var(--text);
    text-decoration: none;
  }

  .wordmark {
    font-size: 1rem;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1;
    overflow-wrap: break-word;
  }

  /* The fixed bottom tab bar. It rides the viewport, not the shell's column,
   * so its inner grid is the one capped at the reading width; the safe-area
   * padding keeps it clear of a phone's home indicator. The shell's own bottom
   * padding in theme.css is what stops content sitting under it. */
  .tab-bar {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 15;
    background: var(--surface);
    border-top: 1px solid var(--line);
    padding:
      0.25rem
      calc(0.5rem + env(safe-area-inset-right))
      calc(0.25rem + env(safe-area-inset-bottom))
      calc(0.5rem + env(safe-area-inset-left));
  }

  .tab-grid {
    max-width: 40rem;
    margin: 0 auto;
    display: grid;
    /* minmax(0, 1fr): a column can never be pushed wider than its share by a
     * long label, which is how the bar stays inside 375 px. */
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 0.25rem;
    min-inline-size: 0;
  }

  .tab {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.15rem;
    min-height: 3.1rem;
    min-inline-size: 0;
    border-radius: var(--radius-control);
    color: var(--dim);
    text-decoration: none;
    font-size: 0.78rem;
    font-weight: 500;
  }

  .tab span {
    text-align: center;
    overflow-wrap: anywhere;
  }

  .tab[aria-current="page"] {
    color: var(--accent);
    background: var(--accent-soft);
  }

  @media (prefers-reduced-motion: no-preference) {
    .tab {
      transition:
        background-color 0.15s ease,
        color 0.15s ease;
    }
  }
</style>
