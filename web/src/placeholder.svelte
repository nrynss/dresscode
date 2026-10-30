<script lang="ts">
  import { packageName } from "@nrynss/chaaya";
  import { Dialog } from "bits-ui";

  let open = $state(false);
  const kit = $derived(packageName);
</script>

<main>
  <h1>Dresscode</h1>
  <p>{kit}</p>
  <Dialog.Root bind:open>
    <Dialog.Trigger>About</Dialog.Trigger>
    <Dialog.Portal>
      <Dialog.Overlay />
      <Dialog.Content>
        <Dialog.Title>Dresscode</Dialog.Title>
        <Dialog.Description>A placeholder page. The dress-code check is not built yet.</Dialog.Description>
        <Dialog.Close>Close</Dialog.Close>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
</main>
