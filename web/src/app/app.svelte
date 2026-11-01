<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import { api } from "../lib/api/client";
  import History from "../routes/history/history.svelte";
  import Occasion from "../routes/occasion/occasion.svelte";
  import Render from "../routes/look/render/render.svelte";
  import Verdict from "../routes/look/verdict/verdict.svelte";
  import { isHistoryHash } from "../routes/history/route";
  import { isRenderHash } from "../routes/look/render/route";
  import { isVerdictHash } from "../routes/look/verdict/route";
  import ErrorNotice from "./error-notice.svelte";
  import Home from "./home.svelte";
  import Shell from "./shell.svelte";
  import "./theme.css";

  /** One sample selfie the judges can open. The id is the key, the name is
   * the one-tap label, and neither is a word the screen owns. */
  interface Sample {
    id: string;
    name: string;
  }

  /** Hash route. The shell has no router. */
  function currentRoute(): string {
    if (typeof window === "undefined") return "/";
    const hash = window.location.hash.replace(/^#/, "");
    if (hash === "/occasion" || hash === "occasion") return "/occasion";
    if (isVerdictHash(hash)) return "/look/verdict";
    if (isRenderHash(hash)) return "/look/render";
    if (isHistoryHash(hash)) return "/history";
    return "/";
  }

  let route = $state(currentRoute());

  function asApiError(error: unknown): ApiError {
    return error instanceof ApiError
      ? error
      : new ApiError("The request could not reach the server.", "network", 0);
  }

  function asString(value: unknown): string | undefined {
    return typeof value === "string" && value !== "" ? value : undefined;
  }

  function asRecord(value: unknown): Record<string, unknown> | undefined {
    if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
    return value as Record<string, unknown>;
  }

  /** The samples, in the order the list returns. A list that cannot be read
   * throws into the usual failure sentence. */
  function samplesOf(body: unknown): Sample[] {
    const list = asRecord(body)?.samples;
    if (!Array.isArray(list)) throw new Error("The sample looks could not be read.");
    const samples: Sample[] = [];
    for (const item of list) {
      const sample = asRecord(item);
      const id = sample === undefined ? undefined : asString(sample.id);
      const name = sample === undefined ? undefined : asString(sample.name);
      if (id === undefined || name === undefined) throw new Error("The sample looks could not be read.");
      samples.push({ id, name });
    }
    return samples;
  }

  let samples = $state<Sample[]>([]);
  let openingSamples = $state(true);
  let sampleProblem = $state<ApiError | null>(null);
  let startingSample = $state(false);

  /** One tap on a sample: the look starts on the server, and the door opens
   * straight onto the finished render. */
  async function startSample(id: string): Promise<void> {
    if (startingSample) return;
    startingSample = true;
    sampleProblem = null;
    try {
      await api<unknown>(`/api/samples/${encodeURIComponent(id)}/looks`, { method: "POST" });
      if (typeof window === "undefined") return;
      window.location.hash = "/look/render";
    } catch (error: unknown) {
      sampleProblem = asApiError(error);
    } finally {
      startingSample = false;
    }
  }

  $effect(() => {
    const control = new AbortController();
    openingSamples = true;
    sampleProblem = null;
    api<unknown>("/api/samples", { signal: control.signal }).then(
      (value) => {
        if (control.signal.aborted) return;
        samples = samplesOf(value);
        openingSamples = false;
      },
      (error: unknown) => {
        if (control.signal.aborted) return;
        sampleProblem = asApiError(error);
        openingSamples = false;
      },
    );
    return () => control.abort();
  });

  $effect(() => {
    if (typeof window === "undefined") return;
    const onChange = () => {
      route = currentRoute();
    };
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  });
</script>

<Shell>
  {#if route === "/occasion"}
    <Occasion />
  {:else if route === "/look/verdict"}
    <Verdict />
  {:else if route === "/look/render"}
    <Render />
  {:else if route === "/history"}
    <History />
  {:else}
    <Home />
    <!-- The occasions are the front door: the cards sit directly on the home
      screen, so picking one needs no extra click. -->
    <Occasion standalone={false} />
    <!-- The judge samples fold away: the drawer opens only for a reader who
      wants one, and the buttons are the honest marker that the list came
      back. -->
    <details class="samples disclosure">
      <summary>Try a sample look</summary>
      {#if sampleProblem}
        <ErrorNotice error={sampleProblem} />
      {:else if openingSamples}
        <p class="quiet" role="status">Opening the samples.</p>
      {:else}
        <div class="sample-row">
          {#each samples as sample (sample.id)}
            <button type="button" disabled={startingSample} onclick={() => void startSample(sample.id)}>
              {sample.name}
            </button>
          {/each}
        </div>
      {/if}
    </details>
  {/if}
</Shell>

<style>
  /* The judge samples fold into the app-wide accordion card, closed until
   * asked. The recipe in theme.css draws the box, the summary row and the
   * body's inset, so this screen keeps only the gap above the card and the
   * rhythm inside the body. */
  .samples {
    margin-top: 2rem;
  }

  .quiet {
    margin: 0 0 0.9rem;
    color: var(--dim);
    overflow-wrap: break-word;
  }

  .sample-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
  }
</style>
