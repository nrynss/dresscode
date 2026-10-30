<script lang="ts">
  import Occasion from "../routes/occasion/occasion.svelte";
  import Verdict from "../routes/look/verdict/verdict.svelte";
  import { isVerdictHash, verdictHash } from "../routes/look/verdict/route";
  import Home from "./home.svelte";
  import Shell from "./shell.svelte";
  import "./theme.css";

  /** Hash route. The shell has no router. */
  function currentRoute(): string {
    if (typeof window === "undefined") return "/";
    const hash = window.location.hash.replace(/^#/, "");
    if (hash === "/occasion" || hash === "occasion") return "/occasion";
    if (isVerdictHash(hash)) return "/look/verdict";
    return "/";
  }

  let route = $state(currentRoute());

  function openOccasion(): void {
    if (typeof window === "undefined") return;
    window.location.hash = "/occasion";
    route = "/occasion";
  }

  function openVerdict(): void {
    if (typeof window === "undefined") return;
    window.location.hash = verdictHash;
    route = "/look/verdict";
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
  {:else if route === "/look/verdict"}
    <Verdict />
  {:else}
    <Home />
    <div class="actions">
      <button type="button" class="primary" onclick={openOccasion}>Choose an occasion</button>
    </div>
    <div class="actions">
      <button type="button" onclick={openVerdict}>See the verdict</button>
    </div>
  {/if}
</Shell>
