<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import ErrorNotice from "../../app/error-notice.svelte";
  import { listOccasions, readOccasion, readOptIn, writeOptIn } from "./menu";
  import type { OccasionDetail, OccasionSummary } from "./types";

  let occasions = $state<OccasionSummary[]>([]);
  let selected = $state("");
  let detail = $state<OccasionDetail | null>(null);
  let ticked = $state<string[]>([]);
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
</script>

<section class="occasion" aria-labelledby="occasion-heading">
  <button type="button" class="back" onclick={back}>Back</button>
  <h2 id="occasion-heading">Occasion</h2>
  <p class="hint">
    Pick an occasion. Then tick the fixes you are open to. Nothing is ticked until you say so, and that choice stays
    with the occasion.
  </p>

  {#if listProblem}
    <ErrorNotice error={listProblem} />
  {:else if openingList}
    <p class="hint">Opening occasions.</p>
  {:else if occasions.length === 0}
    <p class="hint">No occasions are available.</p>
  {:else}
    <fieldset class="menu" disabled={saving}>
      <legend>Occasion</legend>
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
      <p class="hint">Opening the menu.</p>
    {:else}
      <fieldset class="menu" disabled={saving}>
        <legend>Fixes you are open to</legend>
        {#each detail.fixes as fix (fix.id)}
          <label>
            <input type="checkbox" checked={ticked.includes(fix.id)} onchange={() => toggle(fix.id)} />
            {fix.changes}
          </label>
        {/each}
      </fieldset>
      <div class="row">
        <button type="button" class="primary" disabled={saving} onclick={() => void save()}>
          {saving ? "Saving." : "Save"}
        </button>
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

  fieldset.menu {
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: var(--surface);
    margin: 1rem 0 0;
    padding: 0.35rem 0.75rem 0.75rem;
    min-inline-size: 0;
    max-width: 100%;
  }

  fieldset.menu legend {
    color: var(--dim);
    padding: 0 0.3rem;
    font-size: 0.85rem;
  }

  fieldset.menu label {
    display: flex;
    align-items: flex-start;
    gap: 0.65rem;
    min-height: 2.75rem;
    padding: 0.45rem 0.15rem;
    overflow-wrap: break-word;
  }

  fieldset.menu input {
    width: 1.15rem;
    height: 1.15rem;
    margin-top: 0.2rem;
    accent-color: var(--accent);
    flex: none;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    margin-top: 1rem;
  }

  .saved {
    margin: 0.75rem 0 0;
    color: var(--ok);
  }
</style>
