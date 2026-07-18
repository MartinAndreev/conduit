import type { Run } from "../../runs/types/run.js";

export function implementedFeatureIdsFromRuns(
  runs: readonly Run[],
): ReadonlySet<string> {
  return new Set(
    runs
      .filter(
        (run) =>
          run.status === "completed" &&
          run.roles.some(
            (role) => role.name === "reviewer" && role.status === "completed",
          ),
      )
      .map((run) => run.featureId),
  );
}
