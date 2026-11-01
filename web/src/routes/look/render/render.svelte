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
  import ShopSheet from "./sheet.svelte";
  import Swipe from "./swipe.svelte";

  let look = $state<RenderCopy | null | undefined>(undefined);
  let problem = $state<ShownError | undefined>(undefined);
  let saving = $state(false);
  let saved = $state<LookVerdict | undefined>(undefined);

  $effect(() => {
    const control = new AbortController();
    const lookId = storyLookId;
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
  <a class="back" href="#/">Back</a>
  <h2 id="render-heading">The render</h2>

  {#if problem}
    <ErrorNotice error={problem} />
  {:else if look === undefined}
    <p class="hint" role="status">{openingSentence}</p>
  {:else if look === null}
    <p class="hint" role="status">{pendingSentence}</p>
  {/if}

  {#if look}
    {#if look.fix === undefined}
      <!-- The judge flagged nothing: the good outcome. The sentence leads, the
       * original stands under it, and nothing names a fix that does not exist. -->
      <p role="status">{cleanSentence}</p>
      <img class="original" src={look.selfieUrl} alt="Your original." />
    {:else}
      {#if look.render}
        <Swipe before={look.selfieUrl} after={look.render.url} />
      {:else}
        <p class="hint">{suggestionSentence}</p>
        <img class="original" src={look.selfieUrl} alt="Your original." />
      {/if}

      {#if look.reasons.length > 0}
        <details class="disclosure reasons-drawer">
          <summary>What the dress code still asks for</summary>
          <ul>
            {#each look.reasons as reason, index (index)}
              <li>{reason}</li>
            {/each}
          </ul>
        </details>
      {/if}

      <div class="hero">
        <h3 class="hero-label">A fix</h3>
        <p class="hero-fix">{look.fix}</p>
      </div>
    {/if}

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

    {#if saved}
      <p class="hint" role="status">{savedSentences[saved]}</p>
    {/if}

    {#if look.fix !== undefined}
      <div class="shop">
        <ShopSheet lookId={look.lookId} />
      </div>
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

  ul {
    margin: 0;
    padding-left: 1.1rem;
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

  /* The fix is the screen's one hero: its own card, edged in the accent. */
  .hero {
    margin-top: 0.75rem;
    padding: 1rem;
    background: var(--raised);
    border: 1px solid var(--line-soft);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-control);
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
    margin-top: 0.25rem;
  }

  .verdict button {
    min-inline-size: 0;
    overflow-wrap: break-word;
  }

  .shop {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin-top: 0.25rem;
  }
</style>
