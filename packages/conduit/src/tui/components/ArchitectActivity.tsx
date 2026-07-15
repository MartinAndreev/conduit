import { AgentActivity } from "@tui/components/AgentActivity.js";
import { SplitDiff } from "@tui/components/SplitDiff.js";
import type { ArchitectActivityProps } from "@tui/types/architect-activity.js";
import {
  architectActivityCopy,
  architectActivitySummary,
  architectCurrentActivity,
  architectRunningStatus,
} from "@tui/helpers/architect-activity-presentation.js";

export function ArchitectActivity({
  theme,
  events,
  uniqueFiles,
  expandedIndex,
  loading,
  error,
  selectedDiff,
  featureId,
  selectedFileIndex,
  running = false,
}: ArchitectActivityProps) {
  if (loading)
    return (
      <box width="100%" height="100%" backgroundColor={theme.surface.base}>
        <text content={architectActivityCopy.loading} fg={theme.text.muted} />
      </box>
    );
  if (error)
    return (
      <box width="100%" height="100%" backgroundColor={theme.surface.base}>
        <text content={`Error: ${error}`} fg={theme.status.error} />
      </box>
    );
  const expanded = expandedIndex === null ? undefined : events[expandedIndex];
  const latest = events.at(-1);
  const latestTime = latest
    ? new Date(latest.timestamp).toLocaleTimeString()
    : undefined;
  const currentMessage = architectCurrentActivity(featureId, latest?.content);
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <AgentActivity
        role="architect"
        message={currentMessage}
        state={running ? "working" : "completed"}
        mascotRole="architect"
      />
      {running && (
        <text
          content={architectRunningStatus(latestTime)}
          fg={theme.action.attention}
        />
      )}
      <text
        content={architectActivityCopy.keyboardHelp}
        fg={theme.text.muted}
      />
      <text
        content={architectActivitySummary(uniqueFiles.length, events.length)}
        fg={theme.text.strong}
      />
      <box width="100%" flexGrow={1} flexDirection="row" marginTop={1}>
        <box
          width="28%"
          flexDirection="column"
          backgroundColor={theme.surface.raised}
          padding={1}
          marginRight={1}
        >
          <text
            content={architectActivityCopy.changedFilesHeading}
            fg={theme.text.strong}
          />
          {uniqueFiles.map((file, index) => (
            <text
              key={file}
              content={`${index === selectedFileIndex ? "▶" : " "} ${file}`}
              fg={
                index === selectedFileIndex
                  ? theme.action.primary
                  : theme.text.default
              }
            />
          ))}
        </box>
        <box
          width="72%"
          flexDirection="column"
          paddingLeft={1}
          paddingRight={1}
          paddingTop={1}
          backgroundColor={theme.surface.raised}
        >
          <text
            content={architectActivityCopy.heading}
            fg={theme.text.strong}
          />
          {events.length === 0 && (
            <text
              content={architectActivityCopy.emptyActivity}
              fg={theme.text.muted}
            />
          )}
          {events.slice(-8).map((event, index) => (
            <text
              key={`${event.timestamp}-${index}`}
              content={`${event.type.padEnd(12)} ${event.content.slice(0, 90)}`}
              fg={theme.text.default}
            />
          ))}
          {expanded?.diff && <SplitDiff diff={selectedDiff} height={12} />}
        </box>
      </box>
    </box>
  );
}
