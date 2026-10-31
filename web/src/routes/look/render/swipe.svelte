<script lang="ts">
  import { clampedPosition, midPosition, originalPosition, renderPosition } from "./compare";

  let { before, after }: { before: string; after: string } = $props();

  /** How much of the original covers the frame. A figure in state only; the
   * screen never writes it out. */
  let position = $state(midPosition);

  /** The share of the original the frame hides from the right. */
  const clip = $derived(originalPosition - clampedPosition(position));

  function showOriginal(): void {
    position = clampedPosition(originalPosition);
  }

  function showRender(): void {
    position = clampedPosition(renderPosition);
  }
</script>

<div class="swipe">
  <div class="frame">
    <div class="window">
      <img class="layer" src={after} alt="After the fix." />
      <img
        class="layer original"
        src={before}
        alt="Before the fix."
        style:clip-path="inset(0 {clip}% 0 0)"
      />
    </div>
    <span class="divider" style:left="{clampedPosition(position)}%" aria-hidden="true">
      <span class="handle"><span aria-hidden="true">◂ ▸</span></span>
    </span>
    <button type="button" class="badge original" onclick={showOriginal}>Original</button>
    <button type="button" class="badge after" onclick={showRender}>After the fix</button>
    <input
      class="slide"
      type="range"
      min={renderPosition}
      max={originalPosition}
      bind:value={position}
      aria-label="Swipe between the original and the render"
    />
  </div>
</div>

<style>
  .swipe {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    max-width: 100%;
    min-inline-size: 0;
  }

  /* The arched mirror silhouette. A portrait box the spacer carries the
   * height of, capped so the arch never outgrows the screen. The pictures
   * clip to the arch inside their own window, so the corner chips and the
   * split line are never cut by the curve. */
  .frame {
    position: relative;
    width: 100%;
    max-width: min(100%, calc(72dvh * 3 / 4));
    margin-inline: auto;
    min-inline-size: 0;
    border: 2px solid var(--line);
    border-radius: 999px 999px var(--radius-panel) var(--radius-panel);
    background: var(--sunken);
    touch-action: pan-y;
  }

  /* A portrait box for the two pictures. The spacer carries the height. */
  .frame::before {
    content: "";
    display: block;
    padding-top: 133%;
  }

  .window {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    overflow: hidden;
  }

  .layer {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  /* The split: a quiet line where the original ends, and the round handle
   * that rides it. It stops one pixel short of the frame's own line. */
  .divider {
    position: absolute;
    top: 2px;
    bottom: 2px;
    width: 2px;
    margin-left: -1px;
    background: var(--surface);
    border-left: 1px solid var(--line);
    border-right: 1px solid var(--line);
  }

  .handle {
    position: absolute;
    top: 50%;
    left: 50%;
    translate: -50% -50%;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.5rem;
    height: 2.5rem;
    border-radius: 999px;
    background: var(--surface);
    border: 1px solid var(--line);
    color: var(--text);
  }

  /* The corner badges are the two taps of the arch: the picture at either
   * end, without a second row of buttons below. They ride above the range
   * input, so a tap on a badge reaches the button while a drag anywhere
   * else still drives the split. */
  .badge {
    position: absolute;
    top: 0.6rem;
    z-index: 2;
    min-height: 2.75rem;
    padding: 0.15rem 0.6rem;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 999px;
    color: var(--text);
    overflow-wrap: break-word;
  }

  .badge.original {
    left: 0.6rem;
  }

  .badge.after {
    right: 0.6rem;
  }

  .slide {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    appearance: none;
    -webkit-appearance: none;
    background: transparent;
    cursor: ew-resize;
  }

  .slide::-webkit-slider-thumb {
    appearance: none;
    -webkit-appearance: none;
    width: 3rem;
    height: 100%;
    background: transparent;
    cursor: ew-resize;
  }

  .slide::-moz-range-thumb {
    width: 3rem;
    height: 100%;
    border: none;
    background: transparent;
    cursor: ew-resize;
  }

  @media (prefers-reduced-motion: no-preference) {
    .layer.original {
      transition: clip-path 0.2s ease;
    }
  }
</style>
