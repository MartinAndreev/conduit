import type { FeatureReadModel } from "../../domains/features/types/feature-provider.js";
import type { RolePortrait } from "../../domains/roles/types/portrait.js";
import type { Theme } from "../theme.js";

interface FeatureActionsProps {
  feature: FeatureReadModel | undefined;
  portraits: readonly RolePortrait[];
  theme: Theme;
  actionModalOpen: boolean;
  selectedAction: number;
}

const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

export function FeatureActions({
  feature,
  portraits,
  theme,
  actionModalOpen,
  selectedAction,
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
        <text
          content="Tip: Use / to search, arrow keys to navigate, Enter to select."
          fg={theme.text.muted}
        />
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
          <text content=" Press Enter for actions" fg={theme.text.muted} />
          <text content="" />
          <text content=" Roles:" fg={theme.text.strong} />
          {portraits.map((portrait) => (
            <text
              key={portrait.roleName}
              content={`   ${portrait.fallbackGlyph} ${portrait.label}`}
              fg={theme.text.default}
            />
          ))}
        </box>
      )}
    </box>
  );
}
