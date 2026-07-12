import { useCallback, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { ClarificationQuestion } from "@domains/refinement/types/revision.js";
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
  useKeyboard(
    useCallback(
      (event: { name: string }) => {
        if (event.name === "escape" || event.name === "q") onExit();
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
        content="Answer the open product decisions. Ctrl+Enter resumes the architect; q returns without cancelling it."
        fg={theme.text.muted}
      />
      <box
        flexDirection="column"
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        {questions.map((question) => (
          <box key={question.id} flexDirection="column" marginBottom={1}>
            <text
              content={`${question.id} · ${question.question}`}
              fg={theme.text.strong}
            />
            {question.context && (
              <text content={question.context} fg={theme.text.muted} />
            )}
            {question.options.map((option) => (
              <text
                key={option}
                content={`  ○ ${option}`}
                fg={theme.text.default}
              />
            ))}
          </box>
        ))}
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
