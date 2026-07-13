import { useCallback, useState } from "react";
import { useKeyboard } from "@opentui/react";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";
import type { Theme } from "@tui/theme.js";

export function RefinementPacketReview({
  theme,
  content,
  onApprove,
  onRequestChanges,
  onExit,
}: {
  readonly theme: Theme;
  readonly content: {
    spec: string;
    plan: string;
    tasks: string;
    testCases: string;
  };
  readonly onApprove: () => void;
  readonly onRequestChanges: (feedback: string) => void;
  readonly onExit: () => void;
}) {
  const [editingFeedback, setEditingFeedback] = useState(false);
  const [feedback, setFeedback] = useState("");
  useKeyboard(
    useCallback(
      (event: { name: string }) => {
        if (editingFeedback) return;
        if (event.name === "a") onApprove();
        if (event.name === "r") setEditingFeedback(true);
        if (event.name === "q" || event.name === "escape") onExit();
      },
      [editingFeedback, onApprove, onExit],
    ),
  );
  const packet = [content.spec, content.plan, content.tasks, content.testCases]
    .filter(Boolean)
    .join("\n\n---\n\n");
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text content="Architect packet review" fg={theme.action.primary} />
      <text
        content={
          editingFeedback
            ? "Describe what must change, then Ctrl+Enter sends the packet back to the architect."
            : "a: approve packet · r: request changes · q: return home"
        }
        fg={theme.text.muted}
      />
      <box flexGrow={1} marginTop={1} backgroundColor={theme.surface.raised}>
        <MarkdownDocument
          content={packet || "# Packet\n\nNo packet files were produced."}
        />
      </box>
      {editingFeedback && (
        <box height={8} marginTop={1} backgroundColor={theme.surface.raised}>
          <RefinementTextarea
            fieldId="packet-feedback"
            value={feedback}
            placeholder="Explain what is incorrect or missing. The architect will preserve approved decisions."
            onChange={setFeedback}
            onSubmit={() => onRequestChanges(feedback)}
          />
        </box>
      )}
    </box>
  );
}
