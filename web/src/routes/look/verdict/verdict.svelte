<script lang="ts">
  import { isTerminalStatus, type JobStream } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../../app/error-notice.svelte";
  import type { ShownError } from "../../../lib/api/types";
  import type { RenderCopy } from "../render/copy";
  import { loadLook } from "../render/load";
  import { renderHash } from "../render/route";
  import Swipe from "../render/swipe.svelte";
  import {
    cleanSentence,
    openingSentence,
    pausedSentence,
    readingSentence,
    stageOrder,
    stageWord,
    suggestionOnlySentence,
    type VerdictCopy,
  } from "./copy";
  import { followLook, loadVerdict, shownFailure, storyJobId, storyLookId } from "./follow";
  import { occasionHash } from "./route";

  let stream = $state<JobStream | undefined>(undefined);
  let verdict = $state<VerdictCopy | null>(null);
  let problem = $state<ShownError | undefined>(undefined);
  /** The look's pictures, read beside the job: the mirror rides on them.
   * Undefined while it reads, null when there is nothing to show. */
  let look = $state<RenderCopy | null | undefined>(undefined);

  const word = $derived(stream ? stageWord(stream.stage) : "");
  const paused = $derived(stream?.connection === "paused");
  const inProgress = $derived(stream !== undefined && !isTerminalStatus(stream.status));
  /** A job that ended without an error envelope still owes the screen a sentence. */
  const stopped = $derived(
    stream !== undefined && isTerminalStatus(stream.status) && stream.status !== "done" && stream.error === undefined,
  );
  /** Where the look stands on the path. The look is finished once its job is
   * done, or once its verdict is on the screen. */
  const stageIndex = $derived(word === "" ? -1 : stageOrder.indexOf(word));
  const finished = $derived(stream?.status === "done" || verdict !== null);

  $effect(() => {
    const follow = followLook(storyLookId, storyJobId, (opened) => {
      stream = opened;
    });
    return () => follow.close();
  });

  // The mirror is photo-first, so the look's pictures are read beside the
  // job instead of after it. A look that cannot be read stands the mirror
  // down; the verdict's own words carry the screen.
  $effect(() => {
    const control = new AbortController();
    const lookId = storyLookId;
    void loadLook(lookId, control.signal).then(
      (value) => {
        if (control.signal.aborted) return;
        look = value;
      },
      () => {
        if (control.signal.aborted) return;
        look = null;
      },
    );
    return () => control.abort();
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
  <h2 id="verdict-heading">The verdict</h2>

  {#if look?.render}
    <Swipe before={look.selfieUrl} after={look.render.url} />
  {:else if look}
    <div class="mirror-frame"><img src={look.selfieUrl} alt="Your selfie." /></div>
  {/if}

  {#if stream?.error}
    <ErrorNotice error={{ code: stream.error.code }} />
  {:else if stream?.connection === "failed"}
    <ErrorNotice error={{ code: "network" }} />
  {:else if problem}
    <ErrorNotice error={problem} />
  {:else if stopped}
    <ErrorNotice error={{ code: "job_failed" }} />
  {/if}

  {#if word && inProgress}
    <p class="stage" role="status">{word}.</p>
  {:else if !verdict && !paused && inProgress}
    <p class="hint" role="status">{openingSentence}</p>
  {/if}

  {#snippet stages()}
    <ol class="stages" aria-label="Where the look is">
      {#each stageOrder as stage, index (stage)}
        <li
          class:done={finished || (stageIndex >= 0 && index < stageIndex)}
          class:active={inProgress && index === stageIndex}
          aria-current={inProgress && index === stageIndex ? "step" : undefined}
        >
          <span class="mark" aria-hidden="true"></span>
          <span class="word">{stage}</span>
        </li>
      {/each}
    </ol>
  {/snippet}

  {#if stream !== undefined}
    {#if finished}
      <details class="disclosure stages-drawer">
        <summary>Progress</summary>
        {@render stages()}
      </details>
    {:else}
      {@render stages()}
    {/if}
  {/if}

  {#if paused}
    <p class="hint" role="status">{pausedSentence}</p>
  {/if}

  {#if stream?.status === "done" && !verdict && !problem && !stream.error}
    <p class="hint" role="status">{readingSentence}</p>
  {/if}

  {#if verdict}
    {#if verdict.reasons.length > 0}
      <details class="disclosure reasons-drawer">
        <summary>Dress code notes</summary>
        <ul>
          {#each verdict.reasons as reason, index (index)}
            <li>{reason}</li>
          {/each}
        </ul>
      </details>
    {/if}
    {#if verdict.fix === undefined}
      {#if verdict.suggestionOnly}
        <!-- Suggestion-only: the judge flagged the look, but no fix was open
         * to try — nothing ticked on the occasion's fix menu — so no fix was
         * picked. One card, the same recipe the fix card wears: the sentence
         * names the cause in the product's voice, a suggestion and never a
         * score or a blame, and the link is the one-tap path to the
         * occasion's fix menu, where the ticks live. No render link: no
         * render exists to see. -->
        <div class="result-card">
          <div class="hero">
            <h3 class="hero-label">No fix yet</h3>
            <p class="hero-fix">{suggestionOnlySentence}</p>
          </div>
          <a class="primary-link" href={occasionHash}>Choose the fixes to try</a>
        </div>
      {:else}
        <!-- The judge flagged nothing: the good outcome. The sentence stands
         * where the fix card was, and no link points at a render that does
         * not exist. -->
        <p role="status">{cleanSentence}</p>
      {/if}
    {:else}
      <!-- One card for the outcome: the fix and its way forward, in the same
       * language the render screen's Result Card speaks. This screen holds no
       * shop row and no verdict bar to add, so its card carries the fix and
       * the render link alone. -->
      <div class="result-card">
        <div class="hero">
          <h3 class="hero-label">A fix</h3>
          <p class="hero-fix">{verdict.fix}</p>
        </div>
        <a class="primary-link" href={renderHash}>See the render</a>
      </div>
    {/if}
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

  /* The screen opens on the photograph, the way the mirror does on the
   * capture screen: the same arched portrait box, carrying the selfie until
   * the render stands in beside it. */
  .mirror-frame {
    position: relative;
    width: 100%;
    max-width: min(100%, calc(68dvh * 3 / 4));
    margin-inline: auto;
    border: 2px solid var(--line);
    border-radius: 999px 999px var(--radius-panel) var(--radius-panel);
    background: var(--sunken);
    overflow: hidden;
    min-inline-size: 0;
  }

  .mirror-frame::before {
    content: "";
    display: block;
    padding-top: 133%;
  }

  .mirror-frame img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
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
  .stages-drawer > ol {
    margin: 0;
    padding: 0.65rem 0.9rem 0.85rem;
  }

  .reasons-drawer ul {
    list-style: none;
    padding: 0.65rem 0.9rem 0.85rem;
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }

  /* The live path: four words, the one in hand awake, the ones behind it
   * carrying their check, the ones ahead quiet. */
  .stages {
    position: relative;
    display: grid;
    gap: 0.7rem;
    margin: 0.25rem 0 0;
    padding: 0;
    list-style: none;
  }

  .stages li {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.65rem;
    color: var(--dim);
    min-height: 1.55rem;
  }

  .stages li.done,
  .stages li.active {
    color: var(--text);
  }

  .stages li.active {
    color: var(--accent);
  }

  .stages li::before {
    content: "";
    position: absolute;
    top: -0.7rem;
    left: 0.55rem;
    width: 0;
    height: 0.7rem;
    border-left: 1px solid var(--line-soft);
  }

  .stages li:first-child::before {
    content: none;
  }

  .mark {
    position: relative;
    flex: none;
    width: 1.15rem;
    height: 1.15rem;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface);
  }

  .stages li.done .mark {
    background: var(--accent);
    border-color: var(--accent);
  }

  .stages li.active .mark {
    background: var(--accent-soft);
    border-color: var(--accent);
  }

  .mark::before {
    content: "";
    position: absolute;
    inset: 0;
    width: 0.4rem;
    height: 0.6rem;
    margin: auto;
    border-right: 2px solid var(--on-accent);
    border-bottom: 2px solid var(--on-accent);
    transform: rotate(45deg);
    opacity: 0;
  }

  .stages li.done .mark::before {
    opacity: 1;
  }

  .word {
    overflow-wrap: break-word;
  }

  /* One card for the outcome, the same language the render screen's Result
   * Card speaks: the fix and its way forward inside one box edged all the way
   * round in the accent, on the card surface, at the card's own rhythm. The
   * hero keeps no chrome of its own — the card is the card. */
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

  .primary-link {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 2.75rem;
    padding: 0.45rem 0.9rem;
    color: var(--on-accent);
    background: var(--accent);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    text-decoration: none;
    overflow-wrap: break-word;
  }

  .primary-link:hover {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  @media (prefers-reduced-motion: no-preference) {
    .stages li.active .mark {
      animation: stage-pulse 1.6s ease-in-out infinite;
    }
  }

  @keyframes stage-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 0 var(--accent-soft);
    }

    50% {
      box-shadow: 0 0 0 0.35rem var(--accent-soft);
    }
  }
</style>
