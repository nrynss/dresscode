/** The swipe's positions. A position is how much of the original covers the
 * frame, from the render alone to the original alone. It is never written on
 * the screen as a figure. */

/** The frame starts halfway, so the swipe is visible on both pictures. */
export const midPosition = 50;

/** One tap lands here: the original alone, no render in sight. */
export const originalPosition = 100;

/** And back: the render alone. */
export const renderPosition = 0;

/** Keep a position inside the frame. */
export function clampedPosition(value: number): number {
  if (!Number.isFinite(value)) return renderPosition;
  return Math.min(originalPosition, Math.max(renderPosition, value));
}
