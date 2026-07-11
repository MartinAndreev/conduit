import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { Theme } from "@tui/theme.js";

interface FeatureActionsProps {
  feature: FeatureReadModel | undefined;
  theme: Theme;
  actionModalOpen: boolean;
  selectedAction: number;
  tip: string;
  creating: boolean;
  featureTitle: string;
}

const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

export function FeatureActions({
  feature,
  theme,
  actionModalOpen,
  selectedAction,
  tip,
  creating,
  featureTitle,
}: FeatureActionsProps) {
  if (!feature) {
    return (
      <box width="70%" height="100%" flexDirection="column" padding={1}>
        <text content="Welcome to Conduit" fg={theme.text.strong} />
        <text content="" />
        <text
          content={
            creating
              ? `New feature title: ${featureTitle}_`
              : "Press n to create a feature, or select one from the sidebar."
          }
          fg={theme.text.default}
        />
        <text content="" />
        <text content={`Tip: ${tip}`} fg={theme.text.muted} />
      </box>
    );
  }

  return (
    <box width="70%" height="100%" flexDirection="column" padding={1}>
      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={creating ? theme.action.primary : theme.surface.raised}
        paddingLeft={1}
        paddingRight={1}
        marginBottom={1}
      >
        <text content="New feature" fg={theme.action.primary} />
        <text
          content={
            creating
              ? `Title: ${featureTitle}_  · Enter create · Esc cancel`
              : "Press n to define a new feature and begin refinement."
          }
          fg={creating ? theme.text.default : theme.text.muted}
        />
      </box>
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
