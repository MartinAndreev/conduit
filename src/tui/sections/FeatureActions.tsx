import type { FeatureReadModel } from "@domains/features/types/feature.js";
import { AgentActivity } from "@tui/components/AgentActivity.js";
import { AgentEventFeed } from "@tui/components/AgentEventFeed.js";
import type { Theme } from "@tui/theme.js";

interface FeatureActionsProps {
  feature: FeatureReadModel | undefined;
  theme: Theme;
  actionModalOpen: boolean;
  selectedAction: number;
  tip: string;
}

const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

export function FeatureActions({
  feature,
  theme,
  actionModalOpen,
  selectedAction,
  tip,
}: FeatureActionsProps) {
  if (!feature) {
    return (
      <box width="70%" height="100%" flexDirection="column" padding={1}>
        <text content="Welcome to Conduit" fg={theme.text.strong} />
        <text content="" />
        <text
          content="Select a feature from the sidebar to get started."
          fg={theme.text.default}
        />
        <text content="" />
        <text content={`Tip: ${tip}`} fg={theme.text.muted} />
      </box>
    );
  }

  return (
    <box width="70%" height="100%" flexDirection="column" padding={1}>
      <text content={feature.title} fg={theme.text.strong} />
      <text content="" />
      <text
        content={`Status: ${feature.metadata.lifecycle}`}
        fg={
          feature.metadata.lifecycle === "implemented"
            ? theme.action.primary
            : feature.metadata.lifecycle === "in_progress"
              ? theme.action.attention
              : theme.status.error
        }
      />
      <text content="" />
      <text content="Activity" fg={theme.text.strong} />
      <text content="Live runner events appear here." fg={theme.text.muted} />
      <AgentActivity
        role="architect"
        runner="codex"
        message="Inspecting the progress component"
        mascotRole="architect"
      />
      <AgentEventFeed />
      <text content="" />
      {actionModalOpen ? (
        <box
          flexDirection="column"
          borderStyle="single"
          borderColor={theme.action.primary}
          padding={1}
        >
          <text content=" Actions" fg={theme.text.strong} />
          <text content="" />
          {FEATURE_ACTIONS.map((action, index) => (
            <text
              key={action}
              content={`${index === selectedAction ? " > " : "   "}${action}`}
              fg={
                index === selectedAction
                  ? theme.action.primary
                  : theme.text.default
              }
            />
          ))}
        </box>
      ) : (
        <box flexDirection="column">
          <text
            content="Press Enter to choose an action"
            fg={theme.text.muted}
          />
          <text content="" />
          <text content={`Tip: ${tip}`} fg={theme.text.muted} />
        </box>
      )}
    </box>
  );
}
