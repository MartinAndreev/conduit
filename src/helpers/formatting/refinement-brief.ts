export interface RefinementBriefFields {
  readonly problem: string;
  readonly audience: string;
  readonly outcome: string;
  readonly constraints: string;
}

const labels: readonly [keyof RefinementBriefFields, string][] = [
  ["problem", "Problem:"],
  ["audience", "User:"],
  ["outcome", "Desired outcome:"],
  ["constraints", "Constraints and non-goals:"],
];

export function parseRefinementBrief(story: string): RefinementBriefFields {
  const body = story.replace(/^# Story\s*/i, "").trim();
  const result: Record<keyof RefinementBriefFields, string> = {
    problem: "",
    audience: "",
    outcome: "",
    constraints: "",
  };
  for (let index = 0; index < labels.length; index += 1) {
    const [key, label] = labels[index]!;
    const nextLabel = labels[index + 1]?.[1];
    const expression = new RegExp(
      `${label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*([\\s\\S]*?)(?=${nextLabel ? nextLabel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") : "$"})`,
      "i",
    );
    result[key] = body.match(expression)?.[1]?.trim() ?? "";
  }
  if (!result.problem) result.problem = body;
  return result;
}
