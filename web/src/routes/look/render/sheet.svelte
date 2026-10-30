<script lang="ts">
  import { Dialog } from "bits-ui";
  import ErrorNotice from "../../../app/error-notice.svelte";
  import type { ShownError } from "../../../lib/api/types";
  import { shopEmptySentence, shopOpeningSentence, shopSentence, type ShopProduct } from "./copy";
  import { loadShop, shownFailure } from "./load";

  let { lookId }: { lookId: string } = $props();

  let open = $state(false);
  let products = $state<ShopProduct[] | null>(null);
  let problem = $state<ShownError | undefined>(undefined);

  /** The list is read once; an open that failed reads again. */
  $effect(() => {
    if (!open || products !== null) return;
    const control = new AbortController();
    void loadShop(lookId, control.signal).then(
      (value) => {
        if (control.signal.aborted) return;
        products = value.products;
        problem = undefined;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        problem = shownFailure(error);
      },
    );
    return () => control.abort();
  });
</script>

<Dialog.Root bind:open>
  <Dialog.Trigger>Shop the look</Dialog.Trigger>
  <Dialog.Portal>
    <Dialog.Overlay class="dresscode-overlay" />
    <Dialog.Content class="sheet">
      <Dialog.Title class="sheet-title">Shop the look</Dialog.Title>
      <Dialog.Description class="sheet-copy">{shopSentence}</Dialog.Description>

      {#if problem}
        <ErrorNotice error={problem} />
      {:else if products === null}
        <p class="sheet-hint" role="status">{shopOpeningSentence}</p>
      {:else if products.length === 0}
        <p class="sheet-hint">{shopEmptySentence}</p>
      {/if}

      {#if products !== null}
        <ul class="list">
          {#each products as product (product.id)}
            <li class="product">
              {#if product.url}
                <img class="picture" src={product.url} alt={product.name} />
              {/if}
              <h3 class="name">{product.name}</h3>
              <p class="label">{product.label}</p>
            </li>
          {/each}
        </ul>
      {/if}

      <Dialog.Close class="sheet-close">Close</Dialog.Close>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  /* The sheet is the class the dialog carries, so it stays global. */
  :global(.sheet) {
    position: fixed;
    z-index: 21;
    left: 0;
    right: 0;
    bottom: 0;
    width: min(40rem, 100%);
    max-height: calc(100dvh - 4rem);
    overflow: auto;
    margin: 0 auto;
    padding: 1.25rem 1rem calc(1.25rem + env(safe-area-inset-bottom));
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--line);
    border-bottom: none;
    border-radius: var(--radius-panel) var(--radius-panel) 0 0;
  }

  /* The dialog's own parts carry their classes through props, so they stay global. */
  :global(.sheet-title) {
    margin: 0 0 0.25rem;
    font-size: 1.35rem;
    font-weight: 600;
  }

  :global(.sheet-copy) {
    margin: 0 0 0.75rem;
    color: var(--dim);
    overflow-wrap: break-word;
  }

  .sheet-hint {
    margin: 0 0 0.75rem;
    color: var(--dim);
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
  }

  .product {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.2rem;
    padding: 0.75rem;
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-control);
    background: var(--raised);
    max-width: 100%;
    min-inline-size: 0;
  }

  .picture {
    width: 100%;
    height: auto;
    max-height: 12rem;
    object-fit: cover;
    border-radius: var(--radius-control);
  }

  .name {
    margin: 0;
    font-size: 1.15rem;
    font-weight: 600;
    overflow-wrap: break-word;
  }

  .label {
    margin: 0;
    color: var(--text);
    background: var(--accent-soft);
    border: 1px solid var(--line-soft);
    border-radius: var(--radius-control);
    padding: 0.15rem 0.6rem;
    overflow-wrap: break-word;
  }

  :global(.sheet-close) {
    margin-top: 1rem;
  }
</style>
