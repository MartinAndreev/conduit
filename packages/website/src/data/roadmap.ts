export const roadmapPhases = ["shipped", "development", "research"] as const;
export type RoadmapPhase = (typeof roadmapPhases)[number];

export const roadmapPriorities = ["high", "normal"] as const;
export type RoadmapPriority = (typeof roadmapPriorities)[number];

interface RoadmapItemBase {
  readonly id: string;
  readonly title: string;
  readonly outcome: string;
  readonly details: string;
  readonly order: number;
  readonly dependencies?: readonly string[];
}

export interface ShippedRoadmapItem extends RoadmapItemBase {
  readonly phase: "shipped";
  readonly priority?: never;
}

export interface PlannedRoadmapItem extends RoadmapItemBase {
  readonly phase: Exclude<RoadmapPhase, "shipped">;
  readonly priority: RoadmapPriority;
}

export type RoadmapItem = ShippedRoadmapItem | PlannedRoadmapItem;

export interface RoadmapPhaseDefinition {
  readonly id: RoadmapPhase;
  readonly label: string;
  readonly description: string;
}

export const roadmapPhaseDefinitions: readonly RoadmapPhaseDefinition[] = [
  {
    id: "shipped",
    label: "Shipped",
    description:
      "Available in published Conduit releases, within the documented compatibility gates.",
  },
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
      "Shipped in v0.6.0. The update flow shows the running version, asks before making changes, verifies supported standalone downloads, and preserves the existing installation when an update fails.",
    phase: "shipped",
    order: 10,
  },
  {
    id: "local-persistence",
    title: "Local persistence",
    outcome:
      "Keep canonical workflow state in a project-owned local Turso database.",
    details:
      "Conduit persists migrations, runs, results, recovery identity, and related workflow records in local state.db storage. Platform and standalone support remains limited to the combinations covered by release validation.",
    phase: "shipped",
    order: 20,
  },
  {
    id: "universal-agent-communication",
    title: "Universal agent communication",
    outcome:
      "Use one structured assignment and response contract across supported coding harnesses.",
    details:
      "Conduit separates its provider-independent protocol from native harness transport, normalizes runtime events, and validates authoritative final responses. Native adapters remain version and capability gated; this does not claim every provider version is validated.",
    phase: "shipped",
    order: 30,
  },
  {
    id: "durable-role-workspaces",
    title: "Durable role workspaces and recovery",
    outcome:
      "Preserve verifiable role work across interrupted runs and resume only compatible work.",
    details:
      "Shipped in v0.7.0. Conduit records workspace identity and Git lineage, retains recoverable work, and fails closed when a package, commit, assignment, repository, workspace, or lease cannot be verified.",
    phase: "shipped",
    order: 40,
  },
  {
    id: "approval-gated-corrections",
    title: "Approval-gated corrections",
    outcome:
      "Route bounded review corrections and promote completed work only after approval.",
    details:
      "Shipped in v0.7.0. Reviewer findings can return to the owning role for bounded correction, and a run completes only after an approved verdict and verified fast-forward promotion.",
    phase: "shipped",
    order: 50,
  },
  {
    id: "agent-memory",
    title: "Agent long-term memory",
    outcome:
      "Carry verified lessons between independent agent runs without replaying raw transcripts.",
    details:
      "The planned implementation keeps Turso as the canonical source of truth, uses Memvid as the default recoverable local index, and allows a configurable Hindsight adapter. Communication stays provider-independent, and retrieved knowledge must remain bounded, evidence-backed, and invalidated when its sources change.",
    phase: "development",
    priority: "high",
    order: 10,
    dependencies: [
      "Local persistence",
      "Universal agent communication",
      "Memory lifecycle and provider implementation",
    ],
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
