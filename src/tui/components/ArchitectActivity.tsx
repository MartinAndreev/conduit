import type { BoxRenderable } from "@opentui/core";
import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";
import type { Theme } from "../theme.js";
import { AgentActivity } from "@tui/components/AgentActivity.js";

interface ArchitectActivityProps {
  readonly theme: Theme;
  readonly events: readonly ArchitectEvent[];
  readonly uniqueFiles: readonly string[];
  readonly expandedIndex: number | null;
  readonly loading: boolean;
  readonly error: string | null;
  readonly diffContainerRef: { current: BoxRenderable | null };
  readonly featureId: string;
  readonly selectedFileIndex: number;
  readonly running?: boolean;
}

export function ArchitectActivity({
  theme,
  events,
  uniqueFiles,
  expandedIndex,
  loading,
  error,
  diffContainerRef,
  featureId,
  selectedFileIndex,
  running = false,
}: ArchitectActivityProps) {
  if (loading)
    return (
      <box width="100%" height="100%" backgroundColor={theme.surface.base}>
        <text content="Loading architect activity..." fg={theme.text.muted} />
      </box>
    );
  if (error)
    return (
      <box width="100%" height="100%" backgroundColor={theme.surface.base}>
        <text content={`Error: ${error}`} fg={theme.status.error} />
      </box>
    );
  const expanded = expandedIndex === null ? undefined : events[expandedIndex];
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
        runner="runner pending"
        message={`Refining feature ${featureId}`}
        state={running ? "working" : "completed"}
        mascotRole="architect"
      />
      {running && (
        <text
          content="Architect is refining the packet. Activity will appear as its transcript is captured."
          fg={theme.action.attention}
        />
      )}
      <text
        content="↑/↓ select changed file · Enter view diff · q exit"
        fg={theme.text.muted}
      />
      <text
        content={`Changed files: ${uniqueFiles.length} | Events: ${events.length}`}
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
          <text content="Changed files" fg={theme.text.strong} />
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
          <text content="Activity" fg={theme.text.strong} />
          {events.slice(-8).map((event, index) => (
            <text
              key={`${event.timestamp}-${index}`}
              content={`${event.type.padEnd(12)} ${event.content.slice(0, 90)}`}
              fg={theme.text.default}
            />
          ))}
          {expanded?.diff && (
            <box
              width="100%"
              height={12}
              marginTop={1}
              ref={diffContainerRef}
            />
          )}
        </box>
      </box>
    </box>
  );
}
