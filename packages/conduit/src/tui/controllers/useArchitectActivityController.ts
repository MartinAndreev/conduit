import { useCallback, useMemo, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";
import type { ArchitectActivityViewModel } from "@tui/types/architect-activity.js";
import { usePollingQuery } from "@tui/hooks/usePollingQuery.js";
import { useSelectableList } from "@tui/hooks/useSelectableList.js";

function extractFileDiff(patch: string, file: string): string | undefined {
  const sections = patch.split(/(?=^diff --git a\/)/m);
  return sections.find((section) =>
    section.startsWith(`diff --git a/${file} b/${file}`),
  );
}

export function useArchitectActivityController(
  queryBus: QueryBus,
  featureId: string,
  onExit: () => void,
  enabled: boolean,
): ArchitectActivityViewModel {
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);
  const execute = useCallback(async () => {
    const result = await queryBus.execute({
      type: "getArchitectEvents",
      featureId,
    });
    if (!result.success) throw new Error(result.error.message);
    return result.data as {
      events: ArchitectEvent[];
      uniqueFiles: string[];
    };
  }, [featureId, queryBus]);
  const { data, loading, error } = usePollingQuery({
    execute,
    createQuery: useCallback(() => undefined, []),
    project: useCallback(
      (result: { events: ArchitectEvent[]; uniqueFiles: string[] }) => result,
      [],
    ),
    enabled,
    intervalMs: 750,
  });
  const events = data?.events ?? [];
  const uniqueFiles = data?.uniqueFiles ?? [];
  const selectFile = useCallback(
    (index: number) => setSelectedFileIndex(index),
    [],
  );
  const files = useSelectableList({
    itemCount: uniqueFiles.length,
    selectedIndex: selectedFileIndex,
    onSelect: selectFile,
    behavior: "cyclic",
  });

  const onKey = useCallback(
    (event: { name: string }) => {
      if (!enabled) return;
      if (event.name === "escape" && expandedIndex !== null) {
        setExpandedIndex(null);
        return;
      }
      if (event.name === "q" || event.name === "escape") return onExit();
      if (event.name === "up") return files.previous();
      if (event.name === "down") return files.next();
      if (event.name === "return" || event.name === "space") {
        const next = events.findIndex((item) =>
          item.files?.includes(uniqueFiles[selectedFileIndex] ?? ""),
        );
        setExpandedIndex((current) => (current === next ? null : next));
      }
    },
    [
      enabled,
      events,
      expandedIndex,
      files,
      onExit,
      selectedFileIndex,
      uniqueFiles,
    ],
  );
  useKeyboard(onKey);

  const selectedFile = uniqueFiles[selectedFileIndex];
  const expandedDiff =
    expandedIndex === null ? undefined : events[expandedIndex]?.diff;
  const selectedDiff =
    expandedDiff && selectedFile
      ? extractFileDiff(expandedDiff, selectedFile)
      : undefined;
  return useMemo(
    () => ({
      events,
      uniqueFiles,
      expandedIndex,
      loading,
      error,
      selectedFileIndex,
      selectedDiff,
    }),
    [
      events,
      uniqueFiles,
      expandedIndex,
      loading,
      error,
      selectedFileIndex,
      selectedDiff,
    ],
  );
}
