import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { GetReviewResultReadModel } from "@domains/runs/interfaces/queries/get-review-result.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { WorkerMonitorScreen } from "@tui/components/WorkerMonitorScreen.js";
import { ReviewResultPanel } from "@tui/components/ReviewResultPanel.js";
import { useWorkerMonitorController } from "@tui/controllers/useWorkerMonitorController.js";

interface RunScreenProps {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  readonly projectRoot: string;
  readonly runId: string;
  readonly onExit: () => void;
}

export function RunScreen({
  commandBus,
  queryBus,
  projectRoot,
  runId,
  onExit,
}: RunScreenProps) {
  const theme = useTheme();
  const [showReview, setShowReview] = useState(false);
  const [reviewData, setReviewData] = useState<GetReviewResultReadModel | null>(
    null,
  );

  const controller = useWorkerMonitorController(
    queryBus,
    commandBus,
    runId,
    projectRoot,
    onExit,
    true,
  );

  useEffect(() => {
    void queryBus
      .execute({ type: "getReviewResult", runId })
      .then((result) => {
        if (result.success)
          setReviewData(result.data as GetReviewResultReadModel);
      })
      .catch(() => {});
  }, [queryBus, runId]);

  useKeyboard((event: { name: string }) => {
    if (event.name === "v") setShowReview((prev) => !prev);
  });

  const review = reviewData?.review;

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
    >
      {showReview && review ? (
        <box flexDirection="column" padding={1}>
          <text content="v toggle review · q exit" fg={theme.text.muted} />
          <ReviewResultPanel
            reviewId={review.reviewId}
            decision={review.decision}
            findings={review.findings}
            evidencePaths={review.evidencePaths}
            followUp={review.followUp}
            reviewedAt={review.reviewedAt}
            theme={theme}
          />
        </box>
      ) : (
        <box flexDirection="column" width="100%" height="100%">
          <WorkerMonitorScreen
            runId={runId}
            events={controller.events}
            roles={controller.roles}
            selectedRoleIndex={controller.selectedRoleIndex}
            expandedEventIndex={controller.expandedEventIndex}
            transcriptExpanded={controller.transcriptExpanded}
            scrollOffset={controller.scrollOffset}
            changedFiles={controller.changedFiles}
            selectedFileIndex={controller.selectedFileIndex}
            totalAdditions={controller.totalAdditions}
            totalDeletions={controller.totalDeletions}
            selectedFileDiff={controller.selectedFileDiff}
            fileDiffExpanded={controller.fileDiffExpanded}
            loading={controller.loading}
            error={controller.error}
            cancelled={controller.cancelled}
            canResume={controller.canResume}
            showRecovery={controller.showRecovery}
            resumeEligibility={controller.resumeEligibility}
            resuming={controller.resuming}
            resumeError={controller.resumeError}
            focusMode={controller.focusMode}
          />
        </box>
      )}
    </box>
  );
}
