<script lang="ts">
  import { theme } from "@nrynss/chaaya/theme";
  import type { Snippet } from "svelte";
  import Capture from "../routes/capture/capture.svelte";
  import { captureHash, isCaptureHash } from "../routes/capture/route";
  import ThemeControl from "./theme-control.svelte";

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
    <div>
      <h1>Dresscode</h1>
      <p class="tag">See the dress code before they do.</p>
      <a class="bring" href={captureHash}>Bring the look</a>
    </div>
    <ThemeControl />
  </header>
  <main>
    {#if isCaptureHash(route)}
      <Capture />
    {:else}
      {@render children()}
    {/if}
  </main>
</div>

<style>
  .bring {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    color: var(--accent);
  }
</style>
