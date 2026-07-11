import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useKeyboard, useRenderer } from "@opentui/react";
import type { BoxRenderable, DiffRenderable } from "@opentui/core";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";

export interface ArchitectActivityViewModel {
  readonly events: readonly ArchitectEvent[];
  readonly uniqueFiles: readonly string[];
  readonly expandedIndex: number | null;
  readonly loading: boolean;
  readonly error: string | null;
  readonly diffContainerRef: { current: BoxRenderable | null };
  readonly selectedFileIndex: number;
}

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
  const renderer = useRenderer();
  const [events, setEvents] = useState<ArchitectEvent[]>([]);
  const [uniqueFiles, setUniqueFiles] = useState<string[]>([]);
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const diffContainerRef = useRef<BoxRenderable | null>(null);
  const diffRef = useRef<DiffRenderable | null>(null);
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);

  useEffect(() => {
    const load = () =>
      void queryBus
        .execute({ type: "getArchitectEvents", featureId })
        .then((result) => {
          if (result.success) {
            const data = result.data as {
              events: ArchitectEvent[];
              uniqueFiles: string[];
            };
            setEvents(data.events);
            setUniqueFiles(data.uniqueFiles);
          } else setError(result.error.message);
          setLoading(false);
        })
        .catch((reason: unknown) => {
          setError(reason instanceof Error ? reason.message : String(reason));
          setLoading(false);
        });
    load();
    const timer = enabled ? setInterval(load, 750) : undefined;
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [enabled, featureId, queryBus]);

  const onKey = useCallback(
    (event: { name: string }) => {
      if (!enabled) return;
      if (event.name === "q" || event.name === "escape") return onExit();
      if (event.name === "up")
        return setSelectedFileIndex((value) =>
          uniqueFiles.length
            ? (value + uniqueFiles.length - 1) % uniqueFiles.length
            : 0,
        );
      if (event.name === "down")
        return setSelectedFileIndex((value) =>
          uniqueFiles.length ? (value + 1) % uniqueFiles.length : 0,
        );
      if (event.name === "return" || event.name === "space")
        setExpandedIndex(
          events.findIndex((item) =>
            item.files?.includes(uniqueFiles[selectedFileIndex] ?? ""),
          ),
        );
    },
    [enabled, events, onExit, selectedFileIndex, uniqueFiles],
  );
  useKeyboard(onKey);

  const selectedFile = uniqueFiles[selectedFileIndex];
  const expandedDiff =
    expandedIndex === null ? undefined : events[expandedIndex]?.diff;
  const selectedDiff =
    expandedDiff && selectedFile
      ? extractFileDiff(expandedDiff, selectedFile)
      : undefined;
  useEffect(() => {
    let disposed = false;
    const container = diffContainerRef.current;
    if (!container || !selectedDiff) return;
    void import("@opentui/core").then(
      ({ DiffRenderable, RGBA, SyntaxStyle }) => {
        if (disposed) return;
        const style = SyntaxStyle.fromStyles({
          default: { fg: RGBA.fromHex("#E6EDF3") },
          string: { fg: RGBA.fromHex("#A5D6FF") },
          keyword: { fg: RGBA.fromHex("#FF7B72"), bold: true },
        });
        const diff = new DiffRenderable(renderer, {
          id: "architect-event-diff",
          diff: selectedDiff,
          view: "split",
          syncScroll: true,
          width: "100%",
          height: 8,
          syntaxStyle: style,
          showLineNumbers: true,
          wrapMode: "none",
        });
        container.add(diff);
        diffRef.current = diff;
      },
    );
    return () => {
      disposed = true;
      if (diffRef.current) {
        container.remove(diffRef.current);
        diffRef.current.destroy();
        diffRef.current = null;
      }
    };
  }, [renderer, selectedDiff]);
  return useMemo(
    () => ({
      events,
      uniqueFiles,
      expandedIndex,
      loading,
      error,
      diffContainerRef,
      selectedFileIndex,
    }),
    [events, uniqueFiles, expandedIndex, loading, error, selectedFileIndex],
  );
}
