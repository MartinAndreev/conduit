import { useCallback, useState } from "react";
import { useKeyboard } from "@opentui/react";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";
import { useTerminalSubmitKey } from "@tui/hooks/useTerminalSubmitKey.js";
import {
  refinementFeedbackInstructions,
  refinementPacketReviewCopy,
} from "@tui/helpers/refinement-packet-review-presentation.js";
import type { RefinementPacketReviewProps } from "@tui/types/refinement-packet-review.js";

export function RefinementPacketReview({
  theme,
  content,
  onApprove,
  onRequestChanges,
  onExit,
}: RefinementPacketReviewProps) {
  const submitKey = useTerminalSubmitKey();
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
      <box
        width="100%"
        height={4}
        flexDirection="column"
        paddingLeft={1}
        paddingRight={1}
        backgroundColor={theme.surface.raised}
      >
        <text
          height={1}
          content={refinementPacketReviewCopy.heading}
          fg={theme.action.primary}
        />
        {editingFeedback ? (
          <text
            height={1}
            content={refinementFeedbackInstructions(submitKey.label)}
            fg={theme.text.muted}
          />
        ) : (
          <box width="100%" height={1} flexDirection="row">
            <text
              width={22}
              content={refinementPacketReviewCopy.approveControl}
              fg={theme.text.default}
            />
            <text
              width={26}
              content={refinementPacketReviewCopy.requestChangesControl}
              fg={theme.text.default}
            />
            <text
              content={refinementPacketReviewCopy.exitControl}
              fg={theme.text.muted}
            />
          </box>
        )}
      </box>
      <box flexGrow={1} marginTop={1} backgroundColor={theme.surface.raised}>
        <MarkdownDocument
          content={packet || refinementPacketReviewCopy.emptyPacket}
        />
      </box>
      {editingFeedback && (
        <box height={8} marginTop={1} backgroundColor={theme.surface.raised}>
          <RefinementTextarea
            fieldId="packet-feedback"
            value={feedback}
            placeholder={refinementPacketReviewCopy.feedbackPlaceholder}
            onChange={setFeedback}
            onSubmit={() => onRequestChanges(feedback)}
          />
        </box>
      )}
    </box>
  );
}
