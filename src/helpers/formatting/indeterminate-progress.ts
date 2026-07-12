/**
 * Produces a moving terminal track for work whose completion percentage is
 * unknown. The caller advances `frame`; no artificial percentage is shown.
 */
export function formatIndeterminateProgress(
  frame: number,
  width: number,
): string {
  const safeWidth = Math.max(3, width);
  const head = ((frame % safeWidth) + safeWidth) % safeWidth;
  return Array.from({ length: safeWidth }, (_, index) =>
    index >= head && index < head + 3 ? "━" : "─",
  ).join("");
}
