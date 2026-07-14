export const roadmapPhases = ["development", "research"] as const;
export type RoadmapPhase = (typeof roadmapPhases)[number];

export const roadmapPriorities = ["high", "normal"] as const;
export type RoadmapPriority = (typeof roadmapPriorities)[number];

export interface RoadmapItem {
  readonly id: string;
  readonly title: string;
  readonly outcome: string;
  readonly details: string;
  readonly phase: RoadmapPhase;
  readonly priority: RoadmapPriority;
  readonly order: number;
  readonly dependencies?: readonly string[];
}

export interface RoadmapPhaseDefinition {
  readonly id: RoadmapPhase;
  readonly label: string;
  readonly description: string;
}

export const roadmapPhaseDefinitions: readonly RoadmapPhaseDefinition[] = [
  {
    id: "development",
    label: "In development",
    description:
      "Approved work moving through implementation and verification.",
  },
  {
    id: "research",
    label: "Research & planning",
    description:
      "Exploration, contract design, and scope validation before development.",
  },
];

export const roadmapItems: readonly RoadmapItem[] = [
  {
    id: "self-update",
    title: "Self update",
    outcome:
      "Check for stable Conduit releases and guide users through a safe update from Home.",
    details:
      "The update flow will show the running version, ask before making changes, verify standalone downloads, and preserve the existing installation when an update fails.",
    phase: "development",
    priority: "high",
    order: 10,
  },
  {
    id: "agent-memory",
    title: "Agent memory",
    outcome:
      "Give roles bounded, evidence-backed repository context and reliable handoffs.",
    details:
      "Memory stays local and project-scoped, ties every observation to source evidence, and invalidates context when that evidence changes.",
    phase: "development",
    priority: "high",
    order: 20,
    dependencies: ["Local persistence foundation"],
  },
  {
    id: "linear-integration",
    title: "Linear integration",
    outcome:
      "Connect planned Conduit work with Linear issues without weakening local feature contracts.",
    details:
      "Research is focused on authorization, issue mapping, synchronization boundaries, and a provider contract that keeps remote state explicit.",
    phase: "research",
    priority: "high",
    order: 10,
  },
  {
    id: "jira-integration",
    title: "Jira integration",
    outcome:
      "Support Jira-backed planning while preserving Conduit's review and ownership gates.",
    details:
      "Planning covers deployment variants, authentication, field mapping, and how Jira workflows translate into Conduit lifecycle states.",
    phase: "research",
    priority: "high",
    order: 20,
  },
  {
    id: "asana-integration",
    title: "Asana integration",
    outcome:
      "Explore a predictable bridge between Asana tasks and Conduit feature packets.",
    details:
      "Research will begin after the higher-priority provider contracts establish a reusable integration boundary.",
    phase: "research",
    priority: "normal",
    order: 30,
    dependencies: ["Remote provider contract research"],
  },
  {
    id: "clickup-integration",
    title: "ClickUp integration",
    outcome:
      "Explore ClickUp task mapping for teams planning agent work outside the repository.",
    details:
      "The research phase will assess workspace structure, custom fields, authentication, and compatibility with the shared provider boundary.",
    phase: "research",
    priority: "normal",
    order: 40,
    dependencies: ["Remote provider contract research"],
  },
];

export function getRoadmapItemsForPhase(
  phase: RoadmapPhase,
): readonly RoadmapItem[] {
  return roadmapItems
    .filter((item) => item.phase === phase)
    .toSorted((left, right) => left.order - right.order);
}
