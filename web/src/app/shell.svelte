<script lang="ts">
  import { theme } from "@nrynss/chaaya/theme";
  import type { Snippet } from "svelte";
  import ThemeControl from "./theme-control.svelte";

  let { children }: { children: Snippet } = $props();

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
    <ThemeControl />
  </header>
  <main>
    {@render children()}
  </main>
</div>
