<script lang="ts">
  import { ApiError } from "@nrynss/chaaya/keel";
  import { api } from "../lib/api/client";
  import History from "../routes/history/history.svelte";
  import Occasion from "../routes/occasion/occasion.svelte";
  import Render from "../routes/look/render/render.svelte";
  import Verdict from "../routes/look/verdict/verdict.svelte";
  import { historyHash, isHistoryHash } from "../routes/history/route";
  import { isRenderHash, renderHash } from "../routes/look/render/route";
  import { isVerdictHash, verdictHash } from "../routes/look/verdict/route";
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
    <section class="samples" aria-labelledby="samples-heading">
      <h3 id="samples-heading">Sample looks</h3>
      <p class="hint">Open a sample selfie to see a finished look right away.</p>
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
    </section>
    <div class="quiet-row">
      <a href={verdictHash}>See the verdict</a>
      <a href={renderHash}>See the render</a>
      <a href={historyHash}>Style history</a>
    </div>
  {/if}
</Shell>

<style>
  .samples {
    margin-top: 2rem;
  }

  .samples h3 {
    margin: 0;
    font-size: 1.15rem;
    font-weight: 600;
    letter-spacing: -0.02em;
  }

  .hint {
    margin: 0.4rem 0 0;
    color: var(--dim);
    max-width: 36rem;
    overflow-wrap: break-word;
  }

  .quiet {
    margin: 0.5rem 0 0;
    color: var(--dim);
    overflow-wrap: break-word;
  }

  .sample-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
    margin-top: 0.75rem;
  }

  .quiet-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem 1.25rem;
    margin-top: 2.5rem;
    padding-top: 0.5rem;
    border-top: 1px solid var(--line-soft);
  }

  .quiet-row a {
    display: inline-flex;
    align-items: center;
    min-height: 2.75rem;
    color: var(--dim);
    text-decoration: none;
  }

  .quiet-row a:hover {
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }
</style>
