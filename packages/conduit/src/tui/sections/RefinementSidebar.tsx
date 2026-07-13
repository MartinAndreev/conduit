import { ConduitMark } from "@tui/components/ConduitMark.js";
import type { DraftField } from "@domains/refinement/types/draft.js";
import type { Theme } from "@tui/theme.js";

interface RefinementSidebarProps {
  readonly theme: Theme;
  readonly fields: readonly DraftField[];
  readonly values: Record<string, string>;
  readonly activeFieldIndex: number;
}

export function RefinementSidebar({
  theme,
  fields,
  values,
  activeFieldIndex,
}: RefinementSidebarProps) {
  return (
    <box
      width="30%"
      height="100%"
      flexDirection="column"
      borderStyle="single"
      borderColor={theme.text.muted}
    >
      <ConduitMark theme={theme} />
      <text content=" Refinement fields" fg={theme.text.strong} />
      <text content="" />
      {fields.map((field, index) => (
        <text
          key={field.name}
          content={`${index === activeFieldIndex ? "▶" : " "} ${field.label}${field.required ? " *" : ""} · ${values[field.name]?.length ?? 0}`}
          fg={
            index === activeFieldIndex ? theme.action.primary : theme.text.muted
          }
        />
      ))}
      <box flexGrow={1} />
      <box flexDirection="column" paddingLeft={1} paddingBottom={1}>
        <text content="Tab/Shift+Tab  field" fg={theme.text.muted} />
        <text content="Ctrl+Enter     preview" fg={theme.text.muted} />
        <text content="Esc            cancel" fg={theme.text.muted} />
      </box>
    </box>
  );
}
