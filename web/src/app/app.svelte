<script lang="ts">
  import Occasion from "../routes/occasion/occasion.svelte";
  import Home from "./home.svelte";
  import Shell from "./shell.svelte";
  import "./theme.css";

  /** Hash route. The shell has no router; this is the occasion screen. */
  function currentRoute(): string {
    if (typeof window === "undefined") return "/";
    const hash = window.location.hash.replace(/^#/, "");
    if (hash === "/occasion" || hash === "occasion") return "/occasion";
    return "/";
  }

  let route = $state(currentRoute());

  function openOccasion(): void {
    if (typeof window === "undefined") return;
    window.location.hash = "/occasion";
    route = "/occasion";
  }

  $effect(() => {
    if (typeof window === "undefined") return;
    const onChange = () => {
      route = currentRoute();
    };
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  });
</script>

<Shell>
  {#if route === "/occasion"}
    <Occasion />
  {:else}
    <Home />
    <div class="actions">
      <button type="button" class="primary" onclick={openOccasion}>Choose an occasion</button>
    </div>
  {/if}
</Shell>
