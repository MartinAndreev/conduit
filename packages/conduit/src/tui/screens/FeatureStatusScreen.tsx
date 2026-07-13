import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { Run } from "@domains/runs/types/run.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { useTheme } from "@tui/components/ThemeProvider.js";

export function FeatureStatusScreen({
  queryBus,
  projectRoot,
  feature,
  onOpenRun,
  onExit,
}: {
  readonly queryBus: QueryBus;
  readonly projectRoot: string;
  readonly feature: FeatureReadModel;
  readonly onOpenRun: (runId: string) => void;
  readonly onExit: () => void;
}) {
  const theme = useTheme();
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void queryBus
      .execute({ type: "latestRuns", projectRoot })
      .then((result) => {
        if (!result.success) {
          setError(result.error.message);
          return;
        }
        const runs = result.data as Run[];
        setRun(
          runs.find((candidate) => candidate.featureId === feature.id) ?? null,
        );
      })
      .catch((reason: unknown) =>
        setError(reason instanceof Error ? reason.message : String(reason)),
      );
  }, [feature.id, projectRoot, queryBus]);

  useKeyboard((event: { name: string }) => {
    if (event.name === "q" || event.name === "escape") onExit();
    if (event.name === "return" && run) onOpenRun(run.id);
  });

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text content={`Status · ${feature.title}`} fg={theme.action.primary} />
      <text
        content="Enter opens the latest run · q returns home"
        fg={theme.text.muted}
      />
      <box
        flexDirection="column"
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        <text content={`Feature: ${feature.id}`} fg={theme.text.strong} />
        <text
          content={`Lifecycle: ${feature.metadata.lifecycle}`}
          fg={theme.text.default}
        />
        <text
          content={`Updated: ${feature.metadata.updatedAt}`}
          fg={theme.text.muted}
        />
        {error ? (
          <text
            content={`Run status unavailable: ${error}`}
            fg={theme.status.error}
          />
        ) : run ? (
          <>
            <text content="" />
            <text content={`Latest run: ${run.id}`} fg={theme.text.strong} />
            <text
              content={`Run state: ${run.status}`}
              fg={theme.text.default}
            />
            <text
              content={`Roles: ${run.roles.length}`}
              fg={theme.text.default}
            />
          </>
        ) : (
          <>
            <text content="" />
            <text
              content="No runs exist for this feature yet."
              fg={theme.text.muted}
            />
          </>
        )}
      </box>
    </box>
  );
}
