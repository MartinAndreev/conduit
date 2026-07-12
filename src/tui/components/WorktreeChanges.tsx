import type { Theme } from "../theme.js";
import type { ChangedFile } from "@domains/runs/types/review.js";

interface WorktreeChangesProps {
  readonly changedFiles: readonly ChangedFile[];
  readonly selectedFileIndex: number;
  readonly totalAdditions: number;
  readonly totalDeletions: number;
  readonly theme: Theme;
}

export function WorktreeChanges({
  changedFiles,
  selectedFileIndex,
  totalAdditions,
  totalDeletions,
  theme,
}: WorktreeChangesProps) {
  if (changedFiles.length === 0) {
    return (
      <box flexDirection="column">
        <text content="No worktree changes detected" fg={theme.text.muted} />
      </box>
    );
  }
  return (
    <box flexDirection="column">
      <text
        content={`Changed files: ${changedFiles.length} (+${totalAdditions} -${totalDeletions})`}
        fg={theme.text.strong}
      />
      {changedFiles.map((file, index) => (
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
      ))}
    </box>
  );
}
