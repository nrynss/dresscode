<script lang="ts">
  import { humanMessage } from "../lib/api/errors";
  import type { ShownError } from "../lib/api/types";

  let { error }: { error: ShownError } = $props();
  const text = $derived(humanMessage(error));

  let visible = $state(true);
  let timer: ReturnType<typeof setTimeout> | undefined = undefined;

  /** A new error reopens the toast whatever the old one was doing, and the
   * toast closes itself after a few seconds; the close button is the
   * immediate way, for screens that stay put while the reader reads. */
  $effect(() => {
    void text;
    visible = true;
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => (visible = false), 7000);
    return () => {
      if (timer !== undefined) clearTimeout(timer);
    };
  });
</script>

{#if visible}
  <aside class="toast" role="alert">
    <p>{text}</p>
    <button type="button" aria-label="Dismiss the error" onclick={() => (visible = false)}>×</button>
  </aside>
{/if}

<style>
  .toast {
    position: fixed;
    left: 50%;
    bottom: 5.5rem;
    transform: translateX(-50%);
    z-index: 80;
    display: flex;
    align-items: flex-start;
    gap: 0.6rem;
    max-width: min(26rem, calc(100vw - 2rem));
    padding: 0.65rem 0.8rem;
    border-radius: var(--radius-control);
    background: var(--stop-soft);
    border: 1px solid var(--stop);
    color: var(--text);
    box-shadow: 0 0.3rem 0.9rem var(--line-soft);
  }
  .toast p {
    margin: 0;
    font-size: 0.9rem;
    line-height: 1.35;
  }
  .toast button {
    flex: none;
    width: 1.6rem;
    height: 1.6rem;
    margin: -0.1rem -0.2rem -0.1rem 0;
    border: 1px solid var(--stop);
    border-radius: var(--radius-control);
    background: transparent;
    color: inherit;
    font-size: 1rem;
    line-height: 1;
    cursor: pointer;
  }
</style>
