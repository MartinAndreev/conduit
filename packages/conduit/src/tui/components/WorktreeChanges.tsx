import type { Theme } from "../theme.js";
import type { ChangedFile } from "@domains/runs/types/review.js";

interface WorktreeChangesProps {
  readonly changedFiles: readonly ChangedFile[];
  readonly selectedFileIndex: number;
  readonly totalAdditions: number;
  readonly totalDeletions: number;
  readonly theme: Theme;
  readonly maxVisibleFiles?: number;
}

const DEFAULT_MAX_VISIBLE_FILES = 12;

export function WorktreeChanges({
  changedFiles,
  selectedFileIndex,
  totalAdditions,
  totalDeletions,
  theme,
  maxVisibleFiles = DEFAULT_MAX_VISIBLE_FILES,
}: WorktreeChangesProps) {
  if (changedFiles.length === 0) {
    return (
      <box flexDirection="column">
        <text content="No worktree changes detected" fg={theme.text.muted} />
      </box>
    );
  }
  const firstVisibleIndex = Math.max(
    0,
    Math.min(
      selectedFileIndex - Math.floor(maxVisibleFiles / 2),
      changedFiles.length - maxVisibleFiles,
    ),
  );
  const visibleFiles = changedFiles.slice(
    firstVisibleIndex,
    firstVisibleIndex + maxVisibleFiles,
  );
  return (
    <box flexDirection="column">
      <text
        content={`Changed files: ${changedFiles.length} (+${totalAdditions} -${totalDeletions})`}
        fg={theme.text.strong}
      />
      {visibleFiles.map((file, visibleIndex) => {
        const index = firstVisibleIndex + visibleIndex;
        return (
          <box key={file.path} flexDirection="row">
            <text
              content={`${index === selectedFileIndex ? "▶" : " "} ${file.path}`}
              fg={
                index === selectedFileIndex
                  ? theme.action.primary
                  : theme.text.default
              }
            />
            <text
              content={`  +${file.additions} -${file.deletions}`}
              fg={theme.text.muted}
            />
          </box>
        );
      })}
      {changedFiles.length > visibleFiles.length && (
        <text
          content={`Showing ${firstVisibleIndex + 1}-${firstVisibleIndex + visibleFiles.length} of ${changedFiles.length}`}
          fg={theme.text.muted}
        />
      )}
    </box>
  );
}
