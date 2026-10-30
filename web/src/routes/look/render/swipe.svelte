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
    <img class="layer" src={after} alt="After the fix." />
    <img
      class="layer original"
      src={before}
      alt="Before the fix."
      style:clip-path="inset(0 {clip}% 0 0)"
    />
    <input
      class="slide"
      type="range"
      min={renderPosition}
      max={originalPosition}
      bind:value={position}
      aria-label="Swipe between the original and the render"
    />
  </div>
  <div class="tap">
    <button type="button" onclick={showOriginal}>Show the original</button>
    <button type="button" onclick={showRender}>Show the render</button>
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

  .frame {
    position: relative;
    width: 100%;
    max-width: 100%;
    min-inline-size: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    overflow: hidden;
    background: var(--sunken);
    touch-action: pan-y;
  }

  /* A portrait box for the two pictures. The spacer carries the height. */
  .frame::before {
    content: "";
    display: block;
    padding-top: 133%;
  }

  .layer {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
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

  .tap {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
</style>
