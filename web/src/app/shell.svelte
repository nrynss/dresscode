<script lang="ts">
  import { theme } from "@nrynss/chaaya/theme";
  import type { Snippet } from "svelte";
  import Account from "../routes/account/account.svelte";
  import ThemeControl from "./theme-control.svelte";

  let { children }: { children: Snippet } = $props();

  function readHash(): string {
    if (typeof location === "undefined") return "";
    return location.hash;
  }

  let route = $state(readHash());

  $effect(() => {
    if (typeof window === "undefined") return;
    const onHash = () => {
      route = location.hash;
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
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
    <div>
      <h1>Dresscode</h1>
      <p class="tag">See the dress code before they do.</p>
    </div>
    <a class="account-link" href="#account">Account</a>
    <ThemeControl />
  </header>
  <main>
    {#if route === "#account"}
      <Account />
    {:else}
      {@render children()}
    {/if}
  </main>
</div>

<style>
  .account-link {
    color: var(--text);
    min-height: 2.75rem;
    display: inline-flex;
    align-items: center;
  }
</style>
