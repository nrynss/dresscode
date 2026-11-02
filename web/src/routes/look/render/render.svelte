<script lang="ts">
  import ErrorNotice from "../../../app/error-notice.svelte";
  import type { ShownError } from "../../../lib/api/types";
  import {
    cleanSentence,
    lookVerdicts,
    openingSentence,
    pendingSentence,
    savedSentences,
    suggestionSentence,
    verdictWords,
    type LookVerdict,
    type RenderCopy,
  } from "./copy";
  import { loadLook, sendVerdict, shownFailure, storyLookId } from "./load";
  import { keptLook } from "../../capture/upload";
  import ShopSheet from "./sheet.svelte";
  import Swipe from "./swipe.svelte";

  let look = $state<RenderCopy | null | undefined>(undefined);
  let problem = $state<ShownError | undefined>(undefined);
  let saving = $state(false);
  let saved = $state<LookVerdict | undefined>(undefined);

  /** The look this screen renders: the one the capture started when there
   * is one, and the recorded story the samples replay otherwise. */
  const activeLookId = keptLook()?.lookId ?? storyLookId;

  $effect(() => {
    const control = new AbortController();
    const lookId = activeLookId;
    void loadLook(lookId, control.signal).then(
      (value) => {
        if (control.signal.aborted) return;
        look = value;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        problem = shownFailure(error);
      },
    );
    return () => control.abort();
  });

  async function onVerdict(verdict: LookVerdict): Promise<void> {
    const current = look;
    if (saving || !current) return;
    saving = true;
    problem = undefined;
    try {
      saved = await sendVerdict(current.lookId, verdict);
    } catch (error: unknown) {
      problem = shownFailure(error);
    } finally {
      saving = false;
    }
  }
</script>

<section class="render" aria-labelledby="render-heading">
  <h2 id="render-heading">The render</h2>

  {#if problem}
    <ErrorNotice error={problem} />
  {:else if look === undefined}
    <p class="hint" role="status">{openingSentence}</p>
  {:else if look === null}
    <p class="hint" role="status">{pendingSentence}</p>
  {/if}

  {#snippet verdictBar()}
    <div class="verdict">
      {#each lookVerdicts as choice (choice)}
        <button
          type="button"
          class:primary={(saved ?? "kept") === choice}
          disabled={saving}
          onclick={() => void onVerdict(choice)}
        >
          {verdictWords[choice]}
        </button>
      {/each}
    </div>
  {/snippet}

  {#if look}
    {#if look.fix === undefined}
      <!-- The judge flagged nothing: the good outcome. The sentence leads, the
       * original stands under it, and nothing names a fix that does not exist. -->
      <p role="status">{cleanSentence}</p>
      <img class="original" src={look.selfieUrl} alt="Your original." />
      {@render verdictBar()}
    {:else}
      {#if look.render}
        <Swipe before={look.selfieUrl} after={look.render.url} />
      {:else}
        <p class="hint">{suggestionSentence}</p>
        <img class="original" src={look.selfieUrl} alt="Your original." />
      {/if}

      {#if look.reasons.length > 0}
        <details class="disclosure reasons-drawer">
          <summary>Dress code notes</summary>
          <ul>
            {#each look.reasons as reason, index (index)}
              <li>{reason}</li>
            {/each}
          </ul>
        </details>
      {/if}

      <!-- One card for the outcome: the fix, the shop row, and the verdict
       * bar together, instead of a sliver-edged hero with stray buttons
       * below it. -->
      <div class="result-card">
        <div class="hero">
          <h3 class="hero-label">A fix</h3>
          <p class="hero-fix">{look.fix}</p>
        </div>
        <ShopSheet lookId={look.lookId} />
        {@render verdictBar()}
      </div>
    {/if}

    {#if saved}
      <p class="hint" role="status">{savedSentences[saved]}</p>
    {/if}
  {/if}
</section>

<style>
  .render {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    margin-top: 1.25rem;
    max-width: 100%;
    min-inline-size: 0;
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

  .hint {
    color: var(--dim);
  }

  .original {
    width: 100%;
    height: auto;
    max-width: 100%;
    min-inline-size: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
  }

  /* The dress-code notes fold away until asked. The summary is the whole
   * control: a quiet tap target whose tail turns as it opens. */
  .disclosure summary {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 2.75rem;
    cursor: pointer;
    color: var(--text);
    overflow-wrap: break-word;
  }

  .disclosure summary::after {
    content: "▾";
    color: var(--dim);
  }

  .disclosure[open] summary::after {
    rotate: 180deg;
  }

  /* The expanded body keeps the card's own inset, and the notes read as a
   * quiet list, not raw browser bullets. */
  .reasons-drawer ul {
    list-style: none;
    padding: 0.65rem 0.9rem 0.85rem;
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }

  /* One cohesive card for the outcome: the fix, the shop row and the verdict
   * bar together, edged all the way round in the accent. */
  .result-card {
    background: var(--surface);
    border: 2px solid var(--accent);
    border-radius: var(--radius-control);
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    min-inline-size: 0;
  }

  .hero {
    min-inline-size: 0;
  }

  .hero h3 {
    margin: 0;
  }

  .hero p {
    margin: 0.35rem 0 0;
  }

  /* The look's verdict: three equal segments, the one stored (Keep, until a
   * choice is stored) carrying the primary fill. */
  .verdict {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 0.4rem;
  }

  .verdict button {
    min-inline-size: 0;
    overflow-wrap: break-word;
  }
</style>
