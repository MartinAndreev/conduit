import type { ClarificationQuestion } from "../types/revision.js";

export function parseQuestions(
  source: string,
): readonly ClarificationQuestion[] {
  const sections = source.split(/^##\s+/m).slice(1);
  const questions = sections.map((section, index) => {
    const [heading = `Q-${String(index + 1).padStart(3, "0")}`, ...body] =
      section.trim().split("\n");
    const normalizedBody = body
      .join("\n")
      .replace(
        /^\s*\*\*(Question|Why this matters|Context|Options|Smallest unblocker):?\*\*\s*:?\s*(.*)$/gim,
        (_match, label: string, inline: string) =>
          `### ${label}\n${inline.trim()}`,
      )
      .replace(
        /^\s*(Question|Why this matters|Context|Options|Smallest unblocker)\s*:\s*(.*)$/gim,
        (_match, label: string, inline: string) =>
          `### ${label}\n${inline.trim()}`,
      );
    const blocks = normalizedBody.split(/^###\s+/m);
    const unlabelled = blocks[0]?.trim() ?? "";
    const labelled = blocks.slice(1).map((block) => {
      const [label = "", ...content] = block.split("\n");
      return {
        label: label.trim().toLowerCase(),
        content: content.join("\n").trim(),
      };
    });
    const unlabelledParagraphs = unlabelled.split(/\n\s*\n/).filter(Boolean);
    const labelledQuestion = labelled.find(
      ({ label }) => label === "question",
    )?.content;
    const why = labelled.find(
      ({ label }) => label === "why this matters",
    )?.content;
    const explicitContext = labelled.find(
      ({ label }) => label === "context",
    )?.content;
    const context = [
      why ? `**Why this matters**\n\n${why}` : "",
      explicitContext ? `**Context**\n\n${explicitContext}` : "",
      ...unlabelledParagraphs.slice(1),
    ]
      .filter(Boolean)
      .join("\n\n")
      .trim();
    const unblocker = labelled.find(
      ({ label }) => label === "smallest unblocker",
    )?.content;
    const optionSource =
      labelled.find(({ label }) => label === "options")?.content ?? "";
    const options = [
      ...optionSource.matchAll(/^\s*(?:[-*]|\d+\.)\s+(.+)$/gm),
    ].map((match) => match[1]!.trim());
    const headingQuestion = heading
      .replace(/^Q-\d+\s*(?:[—:-]\s*)?/i, "")
      .trim();
    const question =
      labelledQuestion?.trim() ||
      headingQuestion ||
      unlabelledParagraphs[0]?.trim() ||
      heading.trim();
    return {
      id:
        heading.match(/^Q-\d+/i)?.[0] ??
        `Q-${String(index + 1).padStart(3, "0")}`,
      question,
      context,
      ...(unblocker?.trim() ? { unblocker: unblocker.trim() } : {}),
      options,
    };
  });
  return questions.length
    ? questions
    : source.trim()
      ? [{ id: "Q-001", question: source.trim(), options: [] }]
      : [];
}
