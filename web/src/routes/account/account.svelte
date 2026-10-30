<script lang="ts">
  import { Dialog } from "bits-ui";
  import { onDestroy } from "svelte";
  import { ApiError } from "../../lib/api/client";
  import ErrorNotice from "../../app/error-notice.svelte";
  import ThemeControl from "../../app/theme-control.svelte";
  import {
    deleteAccount,
    deleteHistory,
    deleteLook,
    deletePhoto,
    listDeletableLooks,
    readSession,
    requestCode,
    sessionLine,
    verifySignIn,
    type DeletableLook,
    type Session,
    type SignInChoice,
  } from "./flow";
  import { accountSentences, privacyCopy, privacySentences } from "./privacy";
  import Watch from "./watch.svelte";

  const privacy = privacyCopy();
  const sentences = privacySentences();
  const accounts = accountSentences();
  const life = new AbortController();

  let address = $state("");
  let signInCode = $state("");
  let deletionCode = $state("");
  let signInSent = $state(false);
  let freshSent = $state(false);
  let conflict = $state(false);
  let signedIn = $state(false);
  let didSwitch = $state(false);
  let accountErasure = $state(false);
  let pending = $state(false);
  let ready = $state(false);
  let session = $state<Session | null>(null);
  let looks = $state<DeletableLook[]>([]);
  let problem = $state<ApiError | null>(null);
  let jobId = $state<string | null>(null);
  let dialog = $state<string | null>(null);

  const freshCodeReady = $derived(/^\d{6}$/.test(deletionCode.trim()));
  const signInCells = $derived(codeCells(signInCode));
  const deletionCells = $derived(codeCells(deletionCode));

  /** One slot per cell of the code frame. The input keeps the value; the
   * cells only show it. */
  function codeCells(code: string): string[] {
    return Array.from({ length: 6 }, (_, index) => code.slice(index, index + 1));
  }

  $effect(() => {
    const control = new AbortController();
    void boot(control.signal);
    return () => control.abort();
  });

  onDestroy(() => life.abort());

  function isAbort(error: unknown): boolean {
    return error instanceof Error && error.name === "AbortError";
  }

  function asApi(error: unknown): ApiError {
    if (error instanceof ApiError) return error;
    return new ApiError("The request could not reach the server.", "network", 0);
  }

  async function boot(signal: AbortSignal): Promise<void> {
    try {
      session = await readSession(signal);
      looks = await listDeletableLooks(signal);
    } catch (error) {
      if (isAbort(error)) return;
      problem = asApi(error);
    } finally {
      if (!signal.aborted) ready = true;
    }
  }

  async function run(action: () => Promise<string>): Promise<string | undefined> {
    if (pending) return undefined;
    pending = true;
    problem = null;
    try {
      const id = await action();
      jobId = id;
      return id;
    } catch (error) {
      if (!isAbort(error)) problem = asApi(error);
      return undefined;
    } finally {
      pending = false;
    }
  }

  function onSignIn(event: SubmitEvent): void {
    event.preventDefault();
    if (signedIn) return;
    if (!signInSent) void sendSignInCode();
    else void submitCode("");
  }

  async function sendSignInCode(): Promise<void> {
    if (pending || address.trim() === "") return;
    pending = true;
    problem = null;
    conflict = false;
    try {
      await requestCode(address.trim(), life.signal);
      signInSent = true;
      signInCode = "";
    } catch (error) {
      if (!isAbort(error)) problem = asApi(error);
    } finally {
      pending = false;
    }
  }

  async function submitCode(choice: SignInChoice): Promise<void> {
    if (pending || signInCode.trim() === "") return;
    pending = true;
    problem = null;
    try {
      const outcome = await verifySignIn(address.trim(), signInCode.trim(), choice, life.signal);
      if (!outcome.ok) {
        conflict = true;
        problem = new ApiError(
          "Signing in would leave this device's looks on the guest.",
          "guest_data_conflict",
          409,
        );
        return;
      }
      conflict = false;
      signedIn = true;
      didSwitch = outcome.switched;
      signInCode = "";
      session = await readSession(life.signal);
    } catch (error) {
      if (!isAbort(error)) problem = asApi(error);
    } finally {
      pending = false;
    }
  }

  async function sendFreshCode(): Promise<void> {
    if (pending || address.trim() === "") return;
    pending = true;
    problem = null;
    try {
      await requestCode(address.trim(), life.signal);
      freshSent = true;
      deletionCode = "";
    } catch (error) {
      if (!isAbort(error)) problem = asApi(error);
    } finally {
      pending = false;
    }
  }

  async function eraseLook(id: string): Promise<void> {
    const started = await run(() => deleteLook(id, life.signal));
    if (started) looks = looks.filter((look) => look.id !== id);
  }

  async function erasePhoto(id: string): Promise<void> {
    const started = await run(() => deletePhoto(id, life.signal));
    if (started) looks = looks.filter((look) => look.selfieId !== id);
  }

  async function eraseHistory(): Promise<void> {
    await run(() => deleteHistory(life.signal));
  }

  async function eraseAccount(): Promise<void> {
    if (!freshCodeReady) return;
    const started = await run(() => deleteAccount(address.trim(), deletionCode.trim(), life.signal));
    if (!started) return;
    deletionCode = "";
    freshSent = false;
    accountErasure = true;
    signedIn = false;
  }

  function followNext(id: string): void {
    jobId = id;
  }
</script>

<div class="account">
  <p><a href="#home">Back</a></p>
  <h2>Account</h2>

  {#if problem}
    <ErrorNotice error={problem} />
  {/if}

  {#if !ready}
    <p>Opening a session.</p>
  {:else if session}
    <p>{sessionLine(session.kind)}</p>
  {/if}

  <ThemeControl />

  <section class="block" aria-labelledby="sign-in">
    <h3 id="sign-in">Sign in</h3>
    <p>{accounts.keepsLooks}</p>
    <p>A new email address keeps the looks already on this device.</p>
    <p>{accounts.noGoogle}</p>

    {#if signedIn && didSwitch}
      <p>This device now uses your account. Earlier guest looks stayed separate.</p>
    {:else if signedIn}
      <p>You are signed in, and your looks on this device are kept.</p>
    {/if}

    <form onsubmit={onSignIn}>
      <label>
        Email address
        <input type="email" name="email" autocomplete="email" bind:value={address} />
      </label>
      {#if !signedIn && signInSent}
        <label>
          Code
          <span class="otp">
            <input name="code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" bind:value={signInCode} />
            {#each signInCells as cell, index (index)}
              <span class="cell" aria-hidden="true">{cell}</span>
            {/each}
          </span>
        </label>
      {/if}
      {#if !signedIn && !signInSent}
        <button class="primary" type="submit" disabled={pending || address.trim() === ""}>Email a code</button>
      {:else if !signedIn && !conflict}
        <button class="primary" type="submit" disabled={pending || signInCode.trim() === ""}>Sign in</button>
      {/if}
    </form>

    {#if conflict}
      <p>
        Switch moves this device onto the account. The guest's looks stay on the guest. They are not copied, and the
        account's looks are not overwritten.
      </p>
      <div class="row">
        <button class="primary" type="button" disabled={pending} onclick={() => void submitCode("switch")}>
          Switch to the account
        </button>
        <button
          type="button"
          disabled={pending}
          onclick={() => {
            conflict = false;
            if (problem?.code === "guest_data_conflict") problem = null;
          }}
        >
          Stay a guest
        </button>
      </div>
    {/if}
  </section>

  <details open class="disclosure">
    <summary>
      <h2 id="who-sees">{privacy.heading}</h2>
      <span class="chevron" aria-hidden="true">▾</span>
    </summary>
    <div class="block">
      <p>{privacy.cloudflare}</p>
      {#each sentences as sentence (sentence)}
        <p>{sentence}</p>
      {/each}
    </div>
  </details>

  <section class="block" aria-labelledby="deletion">
    <h3 id="deletion">Deletion</h3>
    <p>{accounts.notAutomatic}</p>
    <p>{accounts.stays}</p>

    {#if ready && looks.length === 0}
      <p>Nothing stored to delete yet.</p>
    {/if}

    {#each looks as look (look.id)}
      <article class="block">
        <h3>{look.label}</h3>
        <div class="row">
          <Dialog.Root
            open={dialog === `look:${look.id}`}
            onOpenChange={(open) => {
              if (open) dialog = `look:${look.id}`;
              else if (dialog === `look:${look.id}`) dialog = null;
            }}
          >
            <Dialog.Trigger class="danger" disabled={pending}>Delete this look</Dialog.Trigger>
            <Dialog.Portal>
              <Dialog.Overlay class="dresscode-overlay" />
              <Dialog.Content class="dresscode-panel">
                <Dialog.Title class="dresscode-title">Delete this look</Dialog.Title>
                <Dialog.Description class="dresscode-copy">
                  Removes this look and its renders. The photo stays until you delete it.
                </Dialog.Description>
                <div class="row">
                  <button
                    type="button"
                    class="primary"
                    disabled={pending}
                    onclick={() => {
                      dialog = null;
                      void eraseLook(look.id);
                    }}
                  >
                    Delete
                  </button>
                  <Dialog.Close>Cancel</Dialog.Close>
                </div>
              </Dialog.Content>
            </Dialog.Portal>
          </Dialog.Root>
          {#if look.selfieId}
            {@const selfieId = look.selfieId}
            <Dialog.Root
              open={dialog === `photo:${selfieId}`}
              onOpenChange={(open) => {
                if (open) dialog = `photo:${selfieId}`;
                else if (dialog === `photo:${selfieId}`) dialog = null;
              }}
            >
              <Dialog.Trigger class="danger" disabled={pending}>Delete this photo</Dialog.Trigger>
              <Dialog.Portal>
                <Dialog.Overlay class="dresscode-overlay" />
                <Dialog.Content class="dresscode-panel">
                  <Dialog.Title class="dresscode-title">Delete this photo</Dialog.Title>
                  <Dialog.Description class="dresscode-copy">
                    Removes this photo and the looks that cannot be shown without it.
                  </Dialog.Description>
                  <div class="row">
                    <button
                      type="button"
                      class="primary"
                      disabled={pending}
                      onclick={() => {
                        dialog = null;
                        void erasePhoto(selfieId);
                      }}
                    >
                      Delete
                    </button>
                    <Dialog.Close>Cancel</Dialog.Close>
                  </div>
                </Dialog.Content>
              </Dialog.Portal>
            </Dialog.Root>
          {/if}
        </div>
      </article>
    {/each}

    <Dialog.Root
      open={dialog === "history"}
      onOpenChange={(open) => {
        if (open) dialog = "history";
        else if (dialog === "history") dialog = null;
      }}
    >
      <Dialog.Trigger class="danger" disabled={pending}>Delete style history</Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay class="dresscode-overlay" />
        <Dialog.Content class="dresscode-panel">
          <Dialog.Title class="dresscode-title">Delete style history</Dialog.Title>
          <Dialog.Description class="dresscode-copy">
            Clears style history and turns it off. Looks stay.
          </Dialog.Description>
          <div class="row">
            <button
              type="button"
              class="primary"
              disabled={pending}
              onclick={() => {
                dialog = null;
                void eraseHistory();
              }}
            >
              Delete
            </button>
            <Dialog.Close>Cancel</Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>

    <h3>Delete the account</h3>
    <p>{accounts.deletion}</p>
    <p>Request a fresh code. A code already used to sign in cannot be used again.</p>
    <div class="row">
      <button type="button" disabled={pending || address.trim() === ""} onclick={() => void sendFreshCode()}>
        Email a fresh code
      </button>
    </div>
    {#if freshSent}
      <label>
        Fresh code
        <span class="otp">
          <input
            name="deletion-code"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            bind:value={deletionCode}
          />
          {#each deletionCells as cell, index (index)}
            <span class="cell" aria-hidden="true">{cell}</span>
          {/each}
        </span>
      </label>
      <Dialog.Root
        open={dialog === "account"}
        onOpenChange={(open) => {
          if (open) dialog = "account";
          else if (dialog === "account") dialog = null;
        }}
      >
        <Dialog.Trigger class="danger" disabled={pending || !freshCodeReady}>Delete the account</Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Overlay class="dresscode-overlay" />
          <Dialog.Content class="dresscode-panel">
            <Dialog.Title class="dresscode-title">Delete the account</Dialog.Title>
            <Dialog.Description class="dresscode-copy">{accounts.deletion}</Dialog.Description>
            <div class="row">
              <button
                type="button"
                class="primary"
                disabled={pending}
                onclick={() => {
                  dialog = null;
                  void eraseAccount();
                }}
              >
                Delete the account
              </button>
              <Dialog.Close>Cancel</Dialog.Close>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    {/if}
    {#if accountErasure}
      <p>The session has ended. Deletion continues until every part has confirmed.</p>
    {/if}
  </section>

  {#if jobId}
    {#key jobId}
      <Watch {jobId} onNext={followNext} />
    {/key}
  {/if}
</div>

<style>
  .account {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
    margin-top: 1rem;
    max-width: 36rem;
  }

  .block {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  h2,
  h3,
  p {
    margin: 0;
  }

  h2 {
    font-size: 1.5rem;
  }

  h3 {
    font-size: 1.15rem;
  }

  a {
    color: var(--text);
  }

  form,
  label {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 0.35rem;
    width: 100%;
  }

  form {
    gap: 0.75rem;
  }

  label {
    color: var(--dim);
  }

  input {
    width: 100%;
    min-height: 2.75rem;
    padding: 0.45rem 0.7rem;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
  }

  /* The code frame: six cells, with the real input stretched invisibly over
   * them, so typing, paste and autofill all stay native. */
  .otp {
    position: relative;
    display: flex;
    gap: 0.4rem;
    width: 100%;
    max-width: 17rem;
  }

  .otp .cell {
    flex: 1 1 0;
    min-width: 0;
    min-height: 3rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    font-family: var(--font-numeric);
    font-size: 1.25rem;
  }

  .otp input {
    position: absolute;
    inset: 0;
    width: 100%;
    min-height: 100%;
    padding: 0;
    color: transparent;
    background: none;
    border: none;
    text-align: center;
  }

  .otp:focus-within {
    outline: var(--focus-ring);
    outline-offset: 3px;
  }

  .disclosure {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .disclosure summary {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 2.75rem;
    cursor: pointer;
    list-style: none;
  }

  .disclosure summary::-webkit-details-marker {
    display: none;
  }

  .chevron {
    color: var(--dim);
  }

  .disclosure[open] .chevron {
    rotate: 180deg;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
</style>
