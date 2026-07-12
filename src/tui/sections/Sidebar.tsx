import type { FeatureReadModel } from "@domains/features/types/feature.js";
import { ConduitMark } from "@tui/components/ConduitMark.js";
import type { Theme } from "@tui/theme.js";

interface SidebarProps {
  features: readonly FeatureReadModel[];
  selectedIndex: number;
  searchQuery: string;
  theme: Theme;
}

function lifecycleColor(
  lifecycle: FeatureReadModel["metadata"]["lifecycle"],
  theme: Theme,
): string {
  switch (lifecycle) {
    case "implemented":
      return theme.action.primary;
    case "in_progress":
      return theme.action.attention;
    case "not_started":
      return theme.status.error;
  }
}

export function Sidebar({
  features,
  selectedIndex,
  searchQuery,
  theme,
}: SidebarProps) {
  const filtered = searchQuery
    ? features.filter((f) =>
        f.title.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : features;

  return (
    <box
      width="30%"
      height="100%"
      flexDirection="column"
      borderStyle="single"
      borderColor={theme.text.muted}
    >
      <box flexDirection="column" padding={0}>
        <ConduitMark theme={theme} />
        <text content=" Features" fg={theme.text.strong} />
        <text content="" />
        <text
          content={
            searchQuery ? ` Search: ${searchQuery}` : " Search: (press /)"
          }
          fg={searchQuery ? theme.text.default : theme.text.muted}
        />
        <text content="" />
        {filtered.length === 0 ? (
          <text content="  No features found" fg={theme.text.muted} />
        ) : (
          filtered.map((feature, index) => {
            const isSelected = index === selectedIndex;
            const dotColor = lifecycleColor(feature.metadata.lifecycle, theme);
            return (
              <box
                key={feature.id}
                flexDirection="row"
                backgroundColor={isSelected ? theme.surface.raised : undefined}
              >
                <text content="  " />
                <text content="●" fg={dotColor} />
                <text
                  content={` ${feature.title}`}
                  fg={isSelected ? theme.text.strong : theme.text.default}
                />
              </box>
            );
          })
        )}
      </box>
    </box>
  );
}
