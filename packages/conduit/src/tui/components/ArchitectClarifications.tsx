import { useCallback, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { ClarificationQuestion } from "@domains/refinement/types/revision.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";
import type { Theme } from "@tui/theme.js";

export function ArchitectClarifications({
  theme,
  questions,
  onSubmit,
  onExit,
}: {
  readonly theme: Theme;
  readonly questions: readonly ClarificationQuestion[];
  readonly onSubmit: (answers: string) => void;
  readonly onExit: () => void;
}) {
  const [answers, setAnswers] = useState("");
  const submit = useCallback(() => onSubmit(answers), [answers, onSubmit]);
  const questionsMarkdown = questions
    .map((question) =>
      [
        `## ${question.id}`,
        question.question,
        question.context,
        question.options.length
          ? [
              "### Options",
              ...question.options.map((option) => `- ${option}`),
            ].join("\n")
          : "",
      ]
        .filter(Boolean)
        .join("\n\n"),
    )
    .join("\n\n");
  useKeyboard(
    useCallback(
      (event: { name: string; ctrl: boolean }) => {
        if (event.name === "escape" || (event.ctrl && event.name === "q"))
          onExit();
      },
      [onExit],
    ),
  );
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text content="Architect clarification" fg={theme.action.primary} />
      <text
        content="Answer the open product decisions."
        fg={theme.text.muted}
      />
      <text
        content="Ctrl+Enter resumes the architect · Esc returns."
        fg={theme.text.muted}
      />
      <box
        flexGrow={1}
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        <MarkdownDocument
          content={questionsMarkdown || "## No open questions"}
        />
      </box>
      <text content="Your answers" fg={theme.action.primary} />
      <box height={10} backgroundColor={theme.surface.raised} marginTop={1}>
        <RefinementTextarea
          fieldId="architect-answers"
          value={answers}
          placeholder="State the decision, any constraints, and which option to use…"
          onChange={setAnswers}
          onSubmit={submit}
        />
      </box>
    </box>
  );
}
