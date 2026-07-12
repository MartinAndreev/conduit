import { useCallback, useEffect } from "react";
import type { SelectableListOptions } from "@tui/types/selection.js";

export function normalizeSelectedIndex(
  index: number,
  itemCount: number,
): number {
  if (itemCount <= 0) return 0;
  return Math.max(0, Math.min(index, itemCount - 1));
}

export function moveSelectedIndex(
  index: number,
  itemCount: number,
  direction: "next" | "previous",
  behavior: SelectableListOptions["behavior"],
): number {
  const current = normalizeSelectedIndex(index, itemCount);
  if (itemCount <= 0) return 0;
  if (behavior === "cyclic")
    return direction === "next"
      ? (current + 1) % itemCount
      : (current + itemCount - 1) % itemCount;
  return direction === "next"
    ? Math.min(current + 1, itemCount - 1)
    : Math.max(current - 1, 0);
}

export function useSelectableList({
  itemCount,
  selectedIndex,
  onSelect,
  behavior,
}: SelectableListOptions) {
  useEffect(() => {
    const normalized = normalizeSelectedIndex(selectedIndex, itemCount);
    if (normalized !== selectedIndex) onSelect(normalized);
  }, [itemCount, onSelect, selectedIndex]);

  const next = useCallback(
    () =>
      onSelect(moveSelectedIndex(selectedIndex, itemCount, "next", behavior)),
    [behavior, itemCount, onSelect, selectedIndex],
  );
  const previous = useCallback(
    () =>
      onSelect(
        moveSelectedIndex(selectedIndex, itemCount, "previous", behavior),
      ),
    [behavior, itemCount, onSelect, selectedIndex],
  );
  const reset = useCallback(() => onSelect(0), [onSelect]);

  return { next, previous, reset };
}
