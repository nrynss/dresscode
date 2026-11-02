<script lang="ts">
  import ErrorNotice from "../../app/error-notice.svelte";
  import type { ShownError } from "../../lib/api/types";
  import { verdictHash } from "../look/verdict/route";
  import { keepLook } from "../capture/upload";
  import { emptyVisitSentence, lookLine, openingSentence, openingVisitSentence, type HistoryState, type SecondVisit } from "./copy";
  import { loadSecondVisit, readHistory, shownFailure, writeHistory } from "./flow";

  let switchState = $state<HistoryState | undefined>(undefined);
  let problem = $state<ShownError | undefined>(undefined);
  let visit = $state<SecondVisit | undefined>(undefined);
  let visitProblem = $state<ShownError | undefined>(undefined);
  let openingVisit = $state(false);
  let saving = $state(false);

  /** A look already tried is free to view: the whole look is on the
   * server, so opening it spends nothing. The job id is the look id —
   * the look row is the look, the job is its engine. */
  function openLook(id: string): void {
    keepLook(id, id);
    if (typeof window !== "undefined") window.location.hash = verdictHash;
  }

  $effect(() => {
    const control = new AbortController();
    void readHistory(control.signal).then(
      (value) => {
        if (control.signal.aborted) return;
        switchState = value;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        problem = shownFailure(error);
      },
    );
    return () => control.abort();
  });

  $effect(() => {
    const current = switchState;
    if (current === undefined) {
      visit = undefined;
      visitProblem = undefined;
      openingVisit = false;
      return;
    }
    const control = new AbortController();
    openingVisit = true;
    visitProblem = undefined;
    void loadSecondVisit(control.signal).then(
      (value) => {
        if (control.signal.aborted) return;
        visit = value;
        openingVisit = false;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        visitProblem = shownFailure(error);
        openingVisit = false;
      },
    );
    return () => control.abort();
  });

  async function toggle(checked: boolean): Promise<void> {
    if (saving) return;
    saving = true;
    problem = undefined;
    try {
      switchState = await writeHistory(checked);
    } catch (error: unknown) {
      problem = shownFailure(error);
    } finally {
      saving = false;
    }
  }
</script>

<section class="history" aria-labelledby="history-heading">
  <a class="back" href="#/">Back</a>
  <div class="history-bar">
    <h2 id="history-heading">Style history</h2>
    {#if switchState !== undefined}
      <label class="switch">
        <input
          type="checkbox"
          checked={switchState.enabled}
          disabled={saving}
          onchange={(event) => void toggle(event.currentTarget.checked)}
        />
        <span class="sr-only">Style history</span>
      </label>
    {/if}
  </div>

  {#if problem}
    <ErrorNotice error={problem} />
  {:else if switchState === undefined}
    <p class="hint" role="status">{openingSentence}</p>
  {:else}
    <details class="disclosure notice-drawer">
      <summary>How style history works</summary>
      <p class="info-card">{switchState.notice}</p>
    </details>

    {#if visitProblem}
      <ErrorNotice error={visitProblem} />
    {:else if openingVisit || visit === undefined}
      <p class="hint" role="status">{openingVisitSentence}</p>
    {:else if visit.groups.length === 0}
      <p class="hint">{emptyVisitSentence}</p>
    {:else}
      {#each visit.groups as group (group.occasion.id)}
        <div class="group">
          <h3>{group.occasion.name}</h3>
          {#if group.line !== ""}
            <p class="line">{group.line}</p>
          {/if}
          <ul>
            {#each group.looks as look (look.id)}
              <li>
                <button type="button" class="look-link" onclick={() => openLook(look.id)}>
                  <span class="look-words">{lookLine(look)}</span>
                  {#if look.verdict !== ""}
                    <span class="tag">{look.verdict}</span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        </div>
      {/each}
    {/if}
  {/if}
</section>

<style>
  .history {
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
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
  }

  /* The header bar: the heading names the screen, the switch sits beside it,
   * and the label's own text is hidden from sight because the heading is the
   * name a reader sees. */
  .history-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.65rem;
    max-width: 100%;
    min-inline-size: 0;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }

  /* The explanation of what the switch keeps is tucked away until asked. */
  .disclosure summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.65rem;
    min-height: 2.75rem;
    padding: 0.45rem 0.15rem;
    max-width: 100%;
    min-inline-size: 0;
    overflow-wrap: break-word;
    color: var(--text);
    cursor: pointer;
    list-style: none;
  }

  .disclosure summary::-webkit-details-marker {
    display: none;
  }

  .disclosure summary::after {
    content: "▾";
    color: var(--dim);
  }

  .disclosure[open] summary::after {
    rotate: 180deg;
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

  .switch {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    min-height: 2.75rem;
    padding: 0.45rem 0.15rem;
    max-width: 100%;
    min-inline-size: 0;
    overflow-wrap: break-word;
  }

  /* The checkbox itself is the pill track; the thumb rides on it, so the
   * label keeps its exact structure and the input stays the one control. */
  .switch input {
    appearance: none;
    position: relative;
    width: 2.75rem;
    height: 1.55rem;
    margin: 0;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--sunken);
    flex: none;
  }

  .switch input::before {
    content: "";
    position: absolute;
    top: 50%;
    left: 0.15rem;
    translate: 0 -50%;
    width: 1.1rem;
    height: 1.1rem;
    border-radius: 999px;
    background: var(--surface);
    border: 1px solid var(--line);
  }

  .switch input:checked {
    background: var(--accent);
    border-color: var(--accent);
  }

  .switch input:checked::before {
    translate: 1.2rem -50%;
    border-color: var(--accent);
  }

  .switch input:disabled {
    opacity: 0.6;
  }

  @media (prefers-reduced-motion: no-preference) {
    .switch input,
    .switch input::before {
      transition:
        background-color 0.15s ease,
        border-color 0.15s ease,
        translate 0.15s ease;
    }
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: var(--surface);
    max-width: 100%;
    min-inline-size: 0;
  }

  .line {
    color: var(--text);
    background: var(--accent-soft);
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-control);
    padding: 0.5rem 0.65rem;
  }

  ul {
    margin: 0;
    padding: 0;
    padding-left: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .tag {
    color: var(--dim);
  }

  /* A look already tried is a door, not a record: the whole look sits on
   * the server, so opening it from here spends nothing. */
  .look-link {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.25rem;
    width: 100%;
    padding: 0.45rem 0.55rem;
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }

  .look-words {
    overflow-wrap: break-word;
    max-width: 100%;
  }

  @media (prefers-reduced-motion: no-preference) {
    .look-link {
      transition:
        border-color 0.15s ease,
        background 0.15s ease,
        transform 0.15s ease;
    }

    .look-link:hover {
      border-color: var(--accent);
      background: var(--accent-soft);
    }

    .look-link:active {
      transform: scale(0.99);
    }
  }
</style>
