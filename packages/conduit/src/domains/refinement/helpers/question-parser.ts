import type { ClarificationQuestion } from "../types/revision.js";

export function parseQuestions(
  source: string,
): readonly ClarificationQuestion[] {
  const sections = source.split(/^##\s+/m).slice(1);
  const questions = sections.map((section, index) => {
    const [heading = `Q-${String(index + 1).padStart(3, "0")}`, ...body] =
      section.trim().split("\n");
    const blocks = body.join("\n").split(/^###\s+/m);
    const unlabelled = blocks[0]?.trim() ?? "";
    const labelled = blocks.slice(1).map((block) => {
      const [label = "", ...content] = block.split("\n");
      return {
        label: label.trim().toLowerCase(),
        content: content.join("\n").trim(),
      };
    });
    const context = labelled
      .filter(
        ({ label }) => label === "why this matters" || label === "context",
      )
      .map(({ content }) => content)
      .join("\n\n")
      .trim();
    const optionSource =
      labelled.find(({ label }) => label === "options")?.content ?? "";
    const options = [
      ...optionSource.matchAll(/^\s*(?:[-*]|\d+\.)\s+(.+)$/gm),
    ].map((match) => match[1]!.trim());
    const question =
      heading.replace(/^Q-\d+\s*(?:[—:-]\s*)?/i, "").trim() ||
      unlabelled ||
      heading.trim();
    return {
      id:
        heading.match(/^Q-\d+/i)?.[0] ??
        `Q-${String(index + 1).padStart(3, "0")}`,
      question,
      context,
      options,
    };
  });
  return questions.length
    ? questions
    : source.trim()
      ? [{ id: "Q-001", question: source.trim(), options: [] }]
      : [];
}
