<script lang="ts">
  import ErrorNotice from "../../app/error-notice.svelte";
  import type { ShownError } from "../../lib/api/types";
  import { emptyVisitSentence, lookLine, openingSentence, openingVisitSentence, type HistoryState, type SecondVisit } from "./copy";
  import { loadSecondVisit, readHistory, shownFailure, writeHistory } from "./flow";

  let switchState = $state<HistoryState | undefined>(undefined);
  let problem = $state<ShownError | undefined>(undefined);
  let visit = $state<SecondVisit | undefined>(undefined);
  let visitProblem = $state<ShownError | undefined>(undefined);
  let openingVisit = $state(false);
  let saving = $state(false);

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
    if (current === undefined || !current.enabled) {
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
  <h2 id="history-heading">Style history</h2>

  {#if problem}
    <ErrorNotice error={problem} />
  {:else if switchState === undefined}
    <p class="hint" role="status">{openingSentence}</p>
  {:else}
    <label class="switch">
      <input
        type="checkbox"
        checked={switchState.enabled}
        disabled={saving}
        onchange={(event) => void toggle(event.currentTarget.checked)}
      />
      Style history
    </label>
    <p class="notice">{switchState.notice}</p>

    {#if switchState.enabled}
      <h3>Past looks</h3>
      {#if visitProblem}
        <ErrorNotice error={visitProblem} />
      {:else if openingVisit || visit === undefined}
        <p class="hint" role="status">{openingVisitSentence}</p>
      {:else if visit.groups.length === 0}
        <p class="hint">{emptyVisitSentence}</p>
      {:else}
        {#each visit.groups as group (group.occasion.id)}
          <div class="group">
            <h4>{group.occasion.name}</h4>
            {#if group.line !== ""}
              <p class="line">{group.line}</p>
            {/if}
            <ul>
              {#each group.looks as look (look.id)}
                <li>
                  <p>{lookLine(look)}</p>
                  {#if look.verdict !== ""}
                    <p class="tag">{look.verdict}</p>
                  {/if}
                </li>
              {/each}
            </ul>
          </div>
        {/each}
      {/if}
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
    margin: 0.75rem 0 0;
    font-size: 1.15rem;
    font-weight: 600;
  }

  h4 {
    margin: 0;
    font-size: 1rem;
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

  .switch {
    display: flex;
    align-items: flex-start;
    gap: 0.65rem;
    min-height: 2.75rem;
    padding: 0.45rem 0.15rem;
    max-width: 100%;
    min-inline-size: 0;
    overflow-wrap: break-word;
  }

  .switch input {
    width: 1.15rem;
    height: 1.15rem;
    margin-top: 0.2rem;
    accent-color: var(--accent);
    flex: none;
  }

  .notice {
    color: var(--dim);
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
</style>
