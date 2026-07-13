import { useEffect, useRef } from "react";
import { useRenderer } from "@opentui/react";
import type { BoxRenderable, TextareaRenderable } from "@opentui/core";

export function RefinementTextarea({
  fieldId,
  value,
  placeholder,
  onChange,
  onSubmit,
  height = "100%",
}: {
  readonly fieldId: string;
  readonly value: string;
  readonly placeholder: string;
  readonly onChange: (value: string) => void;
  readonly onSubmit: () => void;
  readonly height?: number | "auto" | `${number}%`;
}) {
  const renderer = useRenderer();
  const containerRef = useRef<BoxRenderable | null>(null);
  const textareaRef = useRef<TextareaRenderable | null>(null);
  const onChangeRef = useRef(onChange);
  const onSubmitRef = useRef(onSubmit);
  onChangeRef.current = onChange;
  onSubmitRef.current = onSubmit;
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let disposed = false;
    void import("@opentui/core").then(({ TextareaRenderable }) => {
      if (disposed) return;
      const textarea = new TextareaRenderable(renderer, {
        id: `refinement-${fieldId}`,
        width: "100%",
        height,
        initialValue: value,
        placeholder,
        placeholderColor: "#8B8B8B",
        backgroundColor: "#2B332A",
        focusedBackgroundColor: "#2B332A",
        textColor: "#D8D5C8",
        focusedTextColor: "#D8D5C8",
        cursorColor: "#D8C28B",
        wrapMode: "word",
        keyBindings: [
          { name: "return", ctrl: true, action: "submit" },
          { name: "f10", action: "submit" },
        ],
        onContentChange: () => onChangeRef.current(textarea.plainText),
        onSubmit: () => onSubmitRef.current(),
      });
      textarea.traits = {
        capture: ["submit", "navigate"],
        status: "Editing refinement field",
      };
      container.add(textarea);
      textarea.focus();
      textarea.gotoBufferEnd();
      textareaRef.current = textarea;
    });
    return () => {
      disposed = true;
      if (textareaRef.current) {
        container.remove(textareaRef.current);
        textareaRef.current.destroy();
        textareaRef.current = null;
      }
    };
  }, [fieldId, height, renderer]);
  useEffect(() => {
    const textarea = textareaRef.current;
    if (textarea && textarea.plainText !== value) {
      textarea.setText(value);
      textarea.gotoBufferEnd();
      textarea.focus();
    }
  }, [value]);
  return <box ref={containerRef} width="100%" height="100%" />;
}
