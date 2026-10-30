<script lang="ts">
  import type { JobStream } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../../app/error-notice.svelte";
  import type { ShownError } from "../../../lib/api/types";
  import { openingSentence, pausedSentence, readingSentence, stageWord, type VerdictCopy } from "./copy";
  import { followLook, loadVerdict, shownFailure, storyJobId, storyLookId } from "./follow";

  let stream = $state<JobStream | undefined>(undefined);
  let verdict = $state<VerdictCopy | null>(null);
  let problem = $state<ShownError | undefined>(undefined);

  const word = $derived(stream ? stageWord(stream.stage) : "");
  const paused = $derived(stream?.connection === "paused");
  const inProgress = $derived(stream !== undefined && stream.status !== "done" && stream.status !== "error");

  $effect(() => {
    const follow = followLook(storyLookId, storyJobId, (opened) => {
      stream = opened;
    });
    return () => follow.close();
  });

  $effect(() => {
    const current = stream;
    if (!current || current.status !== "done") {
      verdict = null;
      return;
    }
    const control = new AbortController();
    const lookId = storyLookId;
    problem = undefined;
    void loadVerdict(lookId, control.signal).then(
      (value) => {
        if (control.signal.aborted || value.lookId !== lookId) return;
        verdict = value;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        problem = shownFailure(error);
      },
    );
    return () => control.abort();
  });
</script>

<section class="verdict" aria-labelledby="verdict-heading">
  <a class="back" href="#/">Back</a>
  <h2 id="verdict-heading">The verdict</h2>

  {#if stream?.error}
    <ErrorNotice error={{ code: stream.error.code }} />
  {:else if stream?.connection === "failed"}
    <ErrorNotice error={{ code: "network" }} />
  {:else if problem}
    <ErrorNotice error={problem} />
  {/if}

  {#if word && inProgress}
    <p class="stage" role="status">{word}</p>
  {:else if !verdict && !paused && inProgress}
    <p class="hint" role="status">{openingSentence}</p>
  {/if}

  {#if paused}
    <p class="hint" role="status">{pausedSentence}</p>
  {/if}

  {#if stream?.status === "done" && !verdict && !problem && !stream.error}
    <p class="hint" role="status">{readingSentence}</p>
  {/if}

  {#if verdict}
    {#if verdict.reasons.length > 0}
      <h3>What the dress code still asks for</h3>
      <ul>
        {#each verdict.reasons as reason, index (index)}
          <li>{reason}</li>
        {/each}
      </ul>
    {/if}
    <h3>A fix</h3>
    <p>{verdict.fix}</p>
    <h3>Why</h3>
    <p>{verdict.why}</p>
  {/if}
</section>

<style>
  .verdict {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-top: 1.25rem;
    max-width: 100%;
    min-inline-size: 0;
  }

  .back {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    color: var(--accent);
  }

  h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    letter-spacing: -0.02em;
  }

  h3 {
    margin: 0.75rem 0 0;
    font-size: 1.15rem;
    font-weight: 600;
  }

  p,
  li {
    margin: 0;
    max-width: 36rem;
    overflow-wrap: break-word;
  }

  .stage {
    font-size: 1.25rem;
  }

  .hint {
    color: var(--dim);
  }

  ul {
    margin: 0;
    padding-left: 1.1rem;
  }
</style>
