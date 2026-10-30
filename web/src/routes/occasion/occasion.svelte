<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../app/error-notice.svelte";
  import { readHistory } from "../history/flow";
  import { listOccasions, readOccasion, readOptIn, readRecallLine, writeOptIn } from "./menu";
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
  let openingList = $state(true);
  let openingMenu = $state(false);
  let saving = $state(false);
  let savedNote = $state("");
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
    if (saving || selected === id) return;
    selected = id;
  }

  function toggle(id: string): void {
    if (saving) return;
    savedNote = "";
    if (ticked.includes(id)) ticked = ticked.filter((item) => item !== id);
    else ticked = [...ticked, id];
  }

  async function save(): Promise<void> {
    const id = selected;
    const menu = detail;
    if (id === "" || menu === null || saving) return;
    const chosen = menu.fixes.map((fix) => fix.id).filter((fixId) => ticked.includes(fixId));
    saving = true;
    problem = null;
    savedNote = "";
    try {
      const stored = await writeOptIn(id, chosen);
      if (!alive || selected !== id) return;
      ticked = [...stored];
      savedNote = `Saved for ${menu.name}.`;
    } catch (error: unknown) {
      if (!alive || selected !== id) return;
      problem = asApiError(error);
    } finally {
      if (alive && selected === id) saving = false;
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
    detail = null;
    ticked = [];
    savedNote = "";
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

<section class="occasion" aria-labelledby="occasion-heading">
  {#if standalone}
    <button type="button" class="back" onclick={back}>Back</button>
  {/if}
  <h2 id="occasion-heading">Occasion</h2>
  <p class="hint">
    Pick an occasion. Then tick the fixes you are open to. Nothing is ticked until you say so, and that choice stays
    with the occasion.
  </p>

  {#if listProblem}
    <ErrorNotice error={listProblem} />
  {:else if openingList}
    <p class="hint" role="status">Opening occasions.</p>
  {:else if occasions.length === 0}
    <p class="hint">No occasions are available.</p>
  {:else}
    <fieldset class="cards" disabled={saving}>
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
      <fieldset class="menu" disabled={saving}>
        <legend>Fixes you are open to</legend>
        {#each detail.fixes as fix (fix.id)}
          <label>
            <input type="checkbox" checked={ticked.includes(fix.id)} onchange={() => toggle(fix.id)} />
            <span class="tick" aria-hidden="true"></span>
            {fix.changes}
          </label>
        {/each}
      </fieldset>
      <div class="row">
        <button type="button" class="primary" disabled={saving} onclick={() => void save()}>
          {saving ? "Saving." : "Save"}
        </button>
        <a class="btn-primary bring" href="#/capture">Bring the look</a>
      </div>
      {#if savedNote !== ""}
        <p class="saved" role="status">{savedNote}</p>
      {/if}
    {/if}
  {/if}
</section>

<style>
  .occasion {
    margin-top: 1.25rem;
    max-width: 100%;
    min-inline-size: 0;
  }

  .occasion h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    letter-spacing: -0.02em;
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
   * chips carry no boxed fieldset of their own. */
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

  /* Selectable fix chips, with a custom check indicator in place of the
   * native box. */
  fieldset.menu {
    display: grid;
    gap: 0.6rem;
  }

  fieldset.menu label {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 0.65rem;
    min-height: 2.75rem;
    padding: 0.6rem 0.85rem;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    overflow-wrap: break-word;
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
    width: 1.15rem;
    height: 1.15rem;
    margin-top: 0.2rem;
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
    width: 0.4rem;
    height: 0.6rem;
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

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    margin-top: 1rem;
  }

  a.bring {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    padding: 0.45rem 0.9rem;
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    text-decoration: none;
  }

  a.bring:hover {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  .saved {
    margin: 0.75rem 0 0;
    color: var(--ok);
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
