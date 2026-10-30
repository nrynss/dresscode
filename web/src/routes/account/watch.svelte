<script lang="ts">
  import { untrack } from "svelte";
  import { humanMessage } from "../../lib/api/errors";
  import { deletionLines, openDeletionStream } from "./flow";

  let { jobId, onNext }: { jobId: string; onNext?: (id: string) => void } = $props();

  // The parent remounts this view when the job changes, so the first id is the job.
  const stream = untrack(() => openDeletionStream(jobId, (id) => onNext?.(id)));
  stream.attach();

  const lines = $derived(deletionLines(stream.events));
  const failure = $derived(stream.error === undefined ? undefined : humanMessage(stream.error));
  const lost = $derived(lines.length === 0 && failure === undefined && stream.connection === "failed");
</script>

<div class="watch" aria-live="polite">
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {:else if lost}
    <p class="notice" role="alert">The deletion could not be followed.</p>
  {:else if lines.length === 0}
    <p>Waiting for the deletion.</p>
  {:else}
    <ol>
      {#each lines as line, index (`${index}:${line}`)}
        <li>{line}</li>
      {/each}
    </ol>
  {/if}
</div>

<style>
  .watch {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  p,
  ol {
    margin: 0;
  }

  ol {
    padding-left: 1.25rem;
  }
</style>
