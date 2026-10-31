<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../app/error-notice.svelte";
  import { readHistory } from "../history/flow";
  import { fixChipLabel, keepOccasion, listOccasions, readOccasion, readOptIn, readRecallLine, writeOptIn } from "./menu";
  import type { OccasionDetail, OccasionSummary } from "./types";

  /** Standalone carries the way back and the screen heading's own route; the
   * front door embeds the picker, so it keeps the way forward only. */
  let { standalone = true }: { standalone?: boolean } = $props();

  let occasions = $state<OccasionSummary[]>([]);
  let selected = $state("");
  let detail = $state<OccasionDetail | null>(null);
  let ticked = $state<string[]>([]);
  let historyOn = $state(false);
  let line = $state("");
  let listProblem = $state<ApiError | null>(null);
  let problem = $state<ApiError | null>(null);
  let writeProblem = $state<ApiError | null>(null);
  let openingList = $state(true);
  let openingMenu = $state(false);
  let alive = true;

  $effect(() => {
    return () => {
      alive = false;
    };
  });

  function asApiError(error: unknown): ApiError {
    return error instanceof ApiError
      ? error
      : new ApiError("The request could not reach the server.", "network", 0);
  }

  function back(): void {
    if (typeof window === "undefined") return;
    window.location.hash = "/";
  }

  function select(id: string): void {
    if (selected === id) return;
    selected = id;
    keepOccasion(id);
  }

  /** The ticks answer a tap at once, and the set on screen travels to the
   * server behind the scenes. A write already in flight finishes, then the
   * newest set goes in its turn, so the taps land in the order they were
   * made. A write that fails is reported and stays reported until one
   * succeeds. */
  let writing = false;
  let rewrite = false;

  function toggle(id: string): void {
    if (ticked.includes(id)) ticked = ticked.filter((item) => item !== id);
    else ticked = [...ticked, id];
    if (writing) {
      rewrite = true;
      return;
    }
    writing = true;
    void drainWrites();
  }


  async function drainWrites(): Promise<void> {
    try {
      for (;;) {
        rewrite = false;
        const id = selected;
        const menu = detail;
        if (id === "" || menu === null) return;
        const chosen = menu.fixes.map((fix) => fix.id).filter((fixId) => ticked.includes(fixId));
        try {
          await writeOptIn(id, chosen);
        } catch (error: unknown) {
          if (alive && selected === id) writeProblem = asApiError(error);
          return;
        }
        if (!alive) return;
        if (selected !== id && selected !== "") keepOccasion(selected);
        writeProblem = null;
        if (!rewrite) return;
      }
    } finally {
      writing = false;
    }
  }

  $effect(() => {
    const control = new AbortController();
    openingList = true;
    listProblem = null;
    listOccasions(control.signal).then(
      (value) => {
        if (control.signal.aborted || !alive) return;
        occasions = value;
        openingList = false;
      },
      (error: unknown) => {
        if (control.signal.aborted || !alive) return;
        listProblem = asApiError(error);
        openingList = false;
      },
    );
    return () => control.abort();
  });

  /** The switch as the session stores it, read on entry. A failed read keeps
   * the line away: the visit opens without it rather than blocking the menu. */
  $effect(() => {
    const control = new AbortController();
    readHistory(control.signal).then(
      (state) => {
        if (control.signal.aborted || !alive) return;
        historyOn = state.enabled;
      },
      () => {
        if (!control.signal.aborted) historyOn = false;
      },
    );
    return () => control.abort();
  });

  $effect(() => {
    const id = selected;
    if (id === "") return;
    const control = new AbortController();
    openingMenu = true;
    problem = null;
    writeProblem = null;
    detail = null;
    ticked = [];
    Promise.all([readOccasion(id, control.signal), readOptIn(id, control.signal)]).then(
      ([nextDetail, nextTicked]) => {
        if (control.signal.aborted || !alive) return;
        detail = nextDetail;
        ticked = [...nextTicked];
        openingMenu = false;
      },
      (error: unknown) => {
        if (control.signal.aborted || !alive) return;
        problem = asApiError(error);
        openingMenu = false;
      },
    );
    return () => control.abort();
  });

  /** The line the occasion's past visits keep, above the fixes. Asked only
   * while the switch is on, and only for the occasion in hand. A visit the
   * story has not met, or an answer that cannot be read, leaves the menu
   * without the line: picking fixes carries on either way. */
  $effect(() => {
    const id = selected;
    if (id === "" || !historyOn) return;
    const control = new AbortController();
    line = "";
    readRecallLine(id, control.signal).then(
      (next) => {
        if (control.signal.aborted || !alive || selected !== id) return;
        line = next;
      },
      () => {},
    );
    return () => control.abort();
  });
</script>

<section class="occasion">
  {#if standalone}
    <button type="button" class="back" onclick={back}>Back</button>
  {/if}

  {#if listProblem}
    <ErrorNotice error={listProblem} />
  {:else if openingList}
    <p class="hint" role="status">Opening occasions.</p>
  {:else if occasions.length === 0}
    <p class="hint">No occasions are available.</p>
  {:else}
    <fieldset class="cards">
      <legend>Choose an occasion</legend>
      {#each occasions as occasion (occasion.id)}
        <label>
          <input
            type="radio"
            name="occasion"
            value={occasion.id}
            checked={selected === occasion.id}
            onchange={() => select(occasion.id)}
          />
          {occasion.name}
        </label>
      {/each}
    </fieldset>
  {/if}

  {#if selected !== ""}
    {#if problem}
      <ErrorNotice error={problem} />
    {:else if openingMenu || detail === null}
      <p class="hint" role="status">Opening the menu.</p>
    {:else}
      {#if line !== ""}
        <p class="banner" role="status">{line}</p>
      {/if}
      <fieldset class="menu">
        <legend>Fixes you are open to</legend>
        {#each detail.fixes as fix (fix.id)}
          <label>
            <input type="checkbox" checked={ticked.includes(fix.id)} onchange={() => toggle(fix.id)} />
            <span class="tick" aria-hidden="true"></span>
            {fixChipLabel(fix)}
          </label>
        {/each}
      </fieldset>
      {#if writeProblem}
        <ErrorNotice error={writeProblem} />
      {/if}
      <a class="btn-primary bring" href="#/capture">Bring the look</a>
    {/if}
  {/if}
</section>

<style>
  .occasion {
    margin-top: 1.25rem;
    max-width: 100%;
    min-inline-size: 0;
  }

  .hint {
    margin: 0.5rem 0 0;
    color: var(--dim);
    max-width: 36rem;
    overflow-wrap: break-word;
  }

  .back {
    margin: 0 0 1rem;
  }

  /* Both menus rest on a quiet eyebrow legend above them, so the cards and
   * pills carry no boxed fieldset of their own. */
  fieldset.cards,
  fieldset.menu {
    position: relative;
    border: none;
    margin: 1.75rem 0 0;
    padding: 0;
    min-inline-size: 0;
    max-width: 100%;
  }

  fieldset.cards legend,
  fieldset.menu legend {
    position: absolute;
    top: -1.5rem;
    left: 0.15rem;
    color: var(--dim);
    padding: 0;
    font-size: 0.85rem;
  }

  /* Editorial occasion cards. The resting edge is the soft line: the measured
   * line-on-raised pair misses the non-text minimum in the dark theme, so the
   * named line cannot bound a raised card, and the card is identified by its
   * own text. */
  fieldset.cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr));
    gap: 0.6rem;
  }

  fieldset.cards label {
    position: relative;
    display: flex;
    align-items: center;
    min-height: 2.75rem;
    padding: 0.6rem 0.85rem;
    background: var(--raised);
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-control);
    overflow-wrap: break-word;
    cursor: pointer;
  }

  fieldset.cards label:has(input:checked) {
    background: var(--accent-soft);
    border: 1px solid var(--line-soft);
    border-left: 3px solid var(--accent);
  }

  fieldset.cards input,
  fieldset.menu input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
  }

  fieldset.cards label:has(input:focus-visible) {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }

  /* Compact fix pills in a wrapping row. Every colour they draw sits in a
   * pair the contrast table measures: the pill's own text on the surface and
   * on the soft accent fill, the line and the accent against them. */
  fieldset.menu {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  fieldset.menu label {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    min-height: 2.25rem;
    padding: 0.3rem 0.75rem;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 999px;
    cursor: pointer;
  }

  fieldset.menu label:has(input:checked) {
    background: var(--accent-soft);
    border-color: var(--accent);
  }

  fieldset.menu label:has(input:focus-visible) {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }

  .tick {
    position: relative;
    flex: none;
    width: 1rem;
    height: 1rem;
    border: 1px solid var(--line);
    border-radius: 999px;
  }

  fieldset.menu label:has(input:checked) .tick {
    background: var(--accent);
    border-color: var(--accent);
  }

  .tick::before {
    content: "";
    position: absolute;
    inset: 0;
    width: 0.35rem;
    height: 0.55rem;
    margin: auto;
    border-right: 2px solid var(--on-accent);
    border-bottom: 2px solid var(--on-accent);
    transform: rotate(45deg);
    opacity: 0;
  }

  fieldset.menu label:has(input:checked) .tick::before {
    opacity: 1;
  }

  .banner {
    margin: 1.25rem 0 0;
    padding: 0.8rem 0.9rem;
    background: var(--sunken);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    overflow-wrap: break-word;
  }

  a.bring {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    margin-top: 1rem;
    padding: 0.45rem 0.9rem;
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    text-decoration: none;
  }

  a.bring:hover {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  @media (prefers-reduced-motion: no-preference) {
    fieldset.cards label,
    fieldset.menu label,
    fieldset.menu .tick {
      transition:
        background-color 0.15s ease,
        border-color 0.15s ease;
    }
  }
</style>
