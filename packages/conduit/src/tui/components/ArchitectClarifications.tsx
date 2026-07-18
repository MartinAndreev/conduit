import { useCallback, useMemo, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { ClarificationQuestion } from "@domains/refinement/types/revision.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import { RefinementTextarea } from "@tui/components/RefinementTextarea.js";
import type { Theme } from "@tui/theme.js";
import { useTerminalSubmitKey } from "@tui/hooks/useTerminalSubmitKey.js";

const customOptionLabel = "Enter another answer…";

export function renderClarificationAnswers(
  questions: readonly ClarificationQuestion[],
  answers: readonly string[],
): string {
  return questions
    .map(
      (question, index) =>
        `## ${question.id}\n\n${answers[index]?.trim() ?? ""}`,
    )
    .join("\n\n");
}

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
  const submitKey = useTerminalSubmitKey();
  const [questionIndex, setQuestionIndex] = useState(0);
  const [optionIndex, setOptionIndex] = useState(0);
  const [answers, setAnswers] = useState<readonly string[]>(() =>
    questions.map(() => ""),
  );
  const [customAnswer, setCustomAnswer] = useState("");
  const [editingCustom, setEditingCustom] = useState(false);
  const question = questions[questionIndex];
  const options = useMemo(
    () => [...(question?.options ?? []), customOptionLabel],
    [question],
  );

  const moveToQuestion = useCallback(
    (nextIndex: number, nextAnswers: readonly string[]) => {
      if (nextIndex >= questions.length) {
        onSubmit(renderClarificationAnswers(questions, nextAnswers));
        return;
      }
      const nextQuestion = questions[nextIndex];
      const savedAnswer = nextAnswers[nextIndex] ?? "";
      const savedOption = nextQuestion?.options.indexOf(savedAnswer) ?? -1;
      setQuestionIndex(nextIndex);
      setOptionIndex(
        savedOption >= 0 ? savedOption : (nextQuestion?.options.length ?? 0),
      );
      setCustomAnswer(savedOption >= 0 ? "" : savedAnswer);
      setEditingCustom(false);
    },
    [onSubmit, questions],
  );

  const saveAndAdvance = useCallback(
    (answer: string) => {
      const normalized = answer.trim();
      if (!normalized) return;
      const nextAnswers = answers.map((value, index) =>
        index === questionIndex ? normalized : value,
      );
      setAnswers(nextAnswers);
      moveToQuestion(questionIndex + 1, nextAnswers);
    },
    [answers, moveToQuestion, questionIndex],
  );

  const chooseCurrent = useCallback(() => {
    if (!question) return;
    if (optionIndex === question.options.length) {
      const saved = answers[questionIndex] ?? "";
      setCustomAnswer(question.options.includes(saved) ? "" : saved);
      setEditingCustom(true);
      return;
    }
    const selected = question.options[optionIndex];
    if (selected) saveAndAdvance(selected);
  }, [answers, optionIndex, question, questionIndex, saveAndAdvance]);

  useKeyboard(
    useCallback(
      (event: { name: string; ctrl: boolean }) => {
        if (editingCustom) {
          if (event.name === "escape") setEditingCustom(false);
          return;
        }
        if (event.name === "escape" || (event.ctrl && event.name === "q")) {
          onExit();
          return;
        }
        if (event.name === "up" || event.name === "k") {
          setOptionIndex((value) => Math.max(0, value - 1));
          return;
        }
        if (event.name === "down" || event.name === "j") {
          setOptionIndex((value) => Math.min(options.length - 1, value + 1));
          return;
        }
        if (event.name === "left" || event.name === "b") {
          if (questionIndex > 0) moveToQuestion(questionIndex - 1, answers);
          return;
        }
        if (event.name === "return") chooseCurrent();
      },
      [
        answers,
        chooseCurrent,
        editingCustom,
        moveToQuestion,
        onExit,
        options.length,
        questionIndex,
      ],
    ),
  );

  if (!question) {
    return (
      <box
        width="100%"
        height="100%"
        justifyContent="center"
        alignItems="center"
        backgroundColor={theme.surface.base}
      >
        <text
          content="No open clarification questions."
          fg={theme.text.muted}
        />
      </box>
    );
  }

  const questionMarkdown = [
    `## ${question.id} — ${question.question}`,
    question.context ? `### Context\n\n${question.context}` : "",
    question.unblocker ? `### Smallest unblocker\n\n${question.unblocker}` : "",
  ]
    .filter(Boolean)
    .join("\n\n");

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <box height={4} flexDirection="column">
        <text
          height={1}
          content={`Architect clarification · Question ${questionIndex + 1} of ${questions.length}`}
          fg={theme.action.primary}
        />
        <text
          height={1}
          content={
            editingCustom
              ? `Enter your answer · ${submitKey.label} accepts · Esc returns to options`
              : "↑↓ select · Enter accept · ← previous · Esc returns"
          }
          fg={theme.text.muted}
        />
        <text
          height={1}
          content={`${answers.filter((answer) => answer.trim()).length}/${questions.length} answered`}
          fg={theme.text.muted}
        />
      </box>

      <box
        flexGrow={1}
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        <MarkdownDocument content={questionMarkdown} />
      </box>

      {editingCustom ? (
        <>
          <text content="Another answer" fg={theme.action.primary} />
          <box height={8} backgroundColor={theme.surface.raised} marginTop={1}>
            <RefinementTextarea
              fieldId={`architect-answer-${question.id}`}
              value={customAnswer}
              placeholder="State only the decision for this question…"
              onChange={setCustomAnswer}
              onSubmit={() => saveAndAdvance(customAnswer)}
            />
          </box>
        </>
      ) : (
        <box
          height={Math.max(6, Math.min(14, options.length * 3 + 2))}
          flexDirection="column"
        >
          <text content="Choose an answer" fg={theme.action.primary} />
          {options.map((option, index) => {
            const selected = index === optionIndex;
            const savedAnswer = answers[questionIndex] ?? "";
            const alreadyAnswered =
              savedAnswer === option ||
              (index === question.options.length &&
                Boolean(savedAnswer) &&
                !question.options.includes(savedAnswer));
            return (
              <text
                key={`${question.id}-${index}`}
                content={`${selected ? "›" : " "} ${alreadyAnswered ? "✓" : " "} ${option}`}
                fg={selected ? theme.action.primary : theme.text.default}
              />
            );
          })}
        </box>
      )}
    </box>
  );
}
