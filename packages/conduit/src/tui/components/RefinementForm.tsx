import type { Theme } from "../theme.js";
import type { DraftField } from "../../domains/refinement/types/draft.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";

interface RefinementFormProps {
  theme: Theme;
  fields: readonly DraftField[];
  activeFieldIndex: number;
  values: Record<string, string>;
  setActiveValue: (value: string) => void;
  submit: () => void;
  tip: string;
}

export function RefinementForm({
  theme,
  fields,
  activeFieldIndex,
  values,
  setActiveValue,
  submit,
  tip,
}: RefinementFormProps) {
  const activeField = fields[activeFieldIndex];
  const currentValue = activeField ? (values[activeField.name] ?? "") : "";

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      paddingLeft={2}
      paddingRight={2}
    >
      <box
        width="100%"
        height={5}
        flexDirection="row"
        justifyContent="center"
        alignItems="center"
      >
        <text content="Refinement Form" fg={theme.action.primary} />
      </box>

      <box width="100%" height={2} flexDirection="row" justifyContent="center">
        <text
          content="Complete each required field, then press Ctrl+Enter to review your draft."
          fg={theme.text.muted}
        />
      </box>

      <box width="100%" height={2} flexDirection="row" justifyContent="center">
        <text
          content={`Field ${activeFieldIndex + 1} of ${fields.length}: ${activeField?.label ?? ""}`}
          fg={theme.text.strong}
        />
      </box>

      <box
        width="100%"
        height={3}
        flexDirection="row"
        justifyContent="center"
        alignItems="center"
        backgroundColor={theme.surface.raised}
      >
        <text
          content={`${activeField?.guidance ?? ""} Use plain text or Markdown; line breaks are preserved.`}
          fg={theme.text.muted}
        />
      </box>

      <box
        width="100%"
        height={12}
        flexDirection="column"
        backgroundColor={theme.surface.raised}
        marginTop={1}
      >
        <box width="100%" height={1} flexDirection="row">
          <text
            content={`${activeField?.label ?? ""}${activeField?.required ? " *" : ""}`}
            fg={theme.action.primary}
          />
        </box>
        <box
          width="100%"
          height={10}
          paddingLeft={1}
          paddingRight={1}
          paddingTop={1}
        >
          <RefinementTextarea
            fieldId={activeField?.name ?? "field"}
            value={currentValue}
            placeholder={activeField?.guidance ?? "Start typing..."}
            onChange={setActiveValue}
            onSubmit={submit}
          />
        </box>
      </box>
      <box
        width="100%"
        height={3}
        marginTop={1}
        paddingLeft={1}
        paddingRight={1}
      >
        <text content={`Tip: ${tip}`} fg={theme.action.attention} />
      </box>
    </box>
  );
}
