<script lang="ts">
  import { theme } from "@nrynss/chaaya/theme";
  import type { Snippet } from "svelte";
  import Account from "../routes/account/account.svelte";
  import Capture from "../routes/capture/capture.svelte";
  import { captureHash, isCaptureHash } from "../routes/capture/route";

  let { children }: { children: Snippet } = $props();
  let route = $state(currentHash());

  function currentHash(): string {
    if (typeof window === "undefined") return "";
    return window.location.hash;
  }

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
  <header class="shell-header">
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
    <nav class="shell-nav">
      <a class="bring" href={captureHash}>Bring the look</a>
      <a href="#/history">Style history</a>
      <a href="#account">Account</a>
    </nav>
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

<style>
  .shell-header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.25rem 0.65rem;
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    color: var(--text);
    text-decoration: none;
  }

  .wordmark {
    font-size: 1rem;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1;
  }

  .shell-nav {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.25rem 0.65rem;
    font-size: 0.95rem;
  }

  .shell-nav a {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    letter-spacing: -0.02em;
    color: var(--dim);
    text-decoration: none;
  }

  .shell-nav a:hover {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  .shell-nav a.bring {
    color: var(--accent);
  }
</style>
