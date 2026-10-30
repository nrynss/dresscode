<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import { Dialog } from "bits-ui";
  import { api } from "../lib/api/client";
  import ErrorNotice from "./error-notice.svelte";

  interface Session {
    kind: string;
  }

  let session = $state<Session | null>(null);
  let problem = $state<ApiError | null>(null);
  let open = $state(false);

  const here = $derived(
    session === null
      ? ""
      : session.kind === "owner"
        ? "You are signed in."
        : session.kind === "guest"
          ? "You are here as a guest."
          : "You have a session.",
  );

  $effect(() => {
    const control = new AbortController();
    api<Session>("/api/session", { signal: control.signal }).then(
      (value) => {
        if (control.signal.aborted) return;
        session = value;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        problem =
          error instanceof ApiError
            ? error
            : new ApiError("The request could not reach the server.", "network", 0);
      },
    );
    return () => control.abort();
  });
</script>

{#if problem}
  <ErrorNotice error={problem} />
{:else if session}
  <p class="session">{here}</p>
{:else}
  <p class="session">Opening a session.</p>
{/if}

<p class="lead">Bring one selfie for an occasion. Dresscode suggests what the dress code still asks for.</p>

<div class="actions">
  <Dialog.Root bind:open>
    <Dialog.Trigger>About</Dialog.Trigger>
    <Dialog.Portal>
      <Dialog.Overlay class="dresscode-overlay" />
      <Dialog.Content class="dresscode-panel">
        <Dialog.Title class="dresscode-title">Dresscode</Dialog.Title>
        <Dialog.Description class="dresscode-copy">
          A private look at the dress code, before anyone else sees the photo. Dresscode suggests a fix. It does not
          grade the photo.
        </Dialog.Description>
        <Dialog.Close class="primary">Close</Dialog.Close>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
</div>
