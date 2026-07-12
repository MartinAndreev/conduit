import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { Theme } from "@tui/theme.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";

interface FeatureActionsProps {
  feature: FeatureReadModel | undefined;
  theme: Theme;
  actionModalOpen: boolean;
  selectedAction: number;
  tip: string;
  creating: boolean;
  featureTitle: string;
  setFeatureTitle: (title: string) => void;
  submitFeature: () => void;
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
  setFeatureTitle,
  submitFeature,
}: FeatureActionsProps) {
  if (!feature) {
    return (
      <box width="70%" height="100%" flexDirection="column" padding={1}>
        <text content="Welcome to Conduit" fg={theme.text.strong} />
        <text content="" />
        {creating ? (
          <>
            <box height={3} marginTop={1}>
              <RefinementTextarea
                fieldId="home-feature-title"
                value={featureTitle}
                placeholder="Feature title"
                onChange={setFeatureTitle}
                onSubmit={submitFeature}
                height={3}
              />
            </box>
            <text
              content="Type a title · Ctrl+Enter create · Esc cancel"
              fg={theme.text.muted}
            />
          </>
        ) : (
          <text
            content="Press n to create a feature, or select one from the sidebar."
            fg={theme.text.default}
          />
        )}
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
        {creating ? (
          <box height={3} marginTop={1}>
            <RefinementTextarea
              fieldId="home-feature-title"
              value={featureTitle}
              placeholder="Feature title"
              onChange={setFeatureTitle}
              onSubmit={submitFeature}
              height={3}
            />
          </box>
        ) : (
          <text
            content="Press n to define a new feature and begin refinement."
            fg={theme.text.muted}
          />
        )}
        {creating && (
          <text
            content="Type a title · Ctrl+Enter create · Esc cancel"
            fg={theme.text.muted}
          />
        )}
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
