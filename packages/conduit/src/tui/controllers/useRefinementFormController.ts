import { useCallback, useEffect, useMemo, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { DraftField } from "@domains/refinement/types/draft.js";
import refinementTips from "@tui/assets/refinement-tips.json" with { type: "json" };
import type { RefinementFormViewModel } from "@tui/types/refinement.js";

export function useRefinementFormController(
  fields: readonly DraftField[],
  initialValues: Record<string, string>,
  onSubmit: (values: Record<string, string>) => void,
  onCancel: () => void,
  enabled: boolean,
): RefinementFormViewModel {
  const [activeFieldIndex, setActiveFieldIndex] = useState(0);
  const [values, setValues] = useState<Record<string, string>>(initialValues);
  const [cursorPosition, setCursorPosition] = useState(0);
  const activeField = fields[activeFieldIndex];
  const tip = useMemo(() => {
    const options =
      refinementTips[activeField?.name as keyof typeof refinementTips] ?? [];
    return (
      options[Math.floor(Math.random() * options.length)] ??
      "Keep the requirement concrete and reviewable."
    );
  }, [activeField?.name]);

  useEffect(() => {
    if (Object.keys(values).length === 0 && Object.keys(initialValues).length) {
      setValues(initialValues);
      setCursorPosition(
        initialValues[fields[activeFieldIndex]?.name ?? ""]?.length ?? 0,
      );
    }
  }, [initialValues, values]);

  const onKey = useCallback(
    (event: { name: string; ctrl: boolean; shift: boolean; meta: boolean }) => {
      if (!enabled) return;
      const key = event.name;
      if (key === "escape") return onCancel();
      if (key === "tab") {
        setActiveFieldIndex(
          (value) =>
            (value + (event.shift ? -1 : 1) + fields.length) % fields.length,
        );
        setCursorPosition(0);
        return;
      }
      if (key === "return" && event.ctrl) {
        if (
          fields.every(
            (field) => !field.required || Boolean(values[field.name]?.trim()),
          )
        )
          onSubmit(values);
        return;
      }
    },
    [enabled, fields, onCancel, onSubmit, values],
  );
  useKeyboard(onKey);
  return useMemo(
    () => ({
      activeFieldIndex,
      values,
      cursorPosition,
      setActiveValue: (value: string) => {
        if (activeField)
          setValues((current) => ({ ...current, [activeField.name]: value }));
      },
      submit: () => onSubmit(values),
      tip,
    }),
    [activeField, activeFieldIndex, values, cursorPosition, tip],
  );
}
