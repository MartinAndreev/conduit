export interface RefinementBriefFields {
  readonly problem: string;
  readonly audience: string;
  readonly outcome: string;
  readonly constraints: string;
  readonly guidelines: string;
}

export function formatRefinementBrief(fields: RefinementBriefFields): string {
  const sections: readonly [string, string][] = [
    ["Problem / user story", fields.problem],
    ["User or audience", fields.audience],
    ["Desired outcome and acceptance criteria", fields.outcome],
    ["Constraints and non-goals", fields.constraints],
    ["Implementation and design guidance", fields.guidelines],
  ];
  return sections
    .filter(([, value]) => value.trim())
    .map(([heading, value]) => `## ${heading}\n\n${value.trim()}`)
    .join("\n\n");
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
    guidelines: "",
  };
  const markdownSections: readonly [
    keyof RefinementBriefFields,
    readonly string[],
  ][] = [
    ["problem", ["Problem / user story", "Problem"]],
    ["audience", ["User or audience", "User", "Audience"]],
    [
      "outcome",
      [
        "Desired outcome and acceptance criteria",
        "Desired outcome",
        "Acceptance criteria",
      ],
    ],
    ["constraints", ["Constraints and non-goals"]],
    ["guidelines", ["Implementation and design guidance"]],
  ];
  for (const [key, headings] of markdownSections) {
    for (const heading of headings) {
      const expression = new RegExp(
        `^## ${heading.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\n([\\s\\S]*?)(?=^## |(?![\\s\\S]))`,
        "im",
      );
      const value = body.match(expression)?.[1]?.trim();
      if (value) {
        result[key] = value;
        break;
      }
    }
  }
  for (let index = 0; index < labels.length; index += 1) {
    const [key, label] = labels[index]!;
    const nextLabel = labels[index + 1]?.[1];
    const expression = new RegExp(
      `${label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*([\\s\\S]*?)(?=${nextLabel ? nextLabel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") : "$"})`,
      "i",
    );
    const value = body.match(expression)?.[1]?.trim();
    if (value) result[key] = value;
  }
  if (!result.problem) result.problem = body;
  return result;
}
