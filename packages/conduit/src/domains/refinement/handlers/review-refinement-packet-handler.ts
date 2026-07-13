import type { CommandHandler } from "@system/bus/command-bus.js";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type { RefinementRevisionRepository } from "@domains/refinement/interfaces/revision-repository.js";
import type {
  ReviewRefinementPacketCommand,
  ReviewRefinementPacketResult,
} from "@domains/refinement/interfaces/commands/review-refinement-packet.js";

export function createReviewRefinementPacketHandler(deps: {
  projectRoot: string;
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  repository: RefinementRevisionRepository;
}): CommandHandler<
  ReviewRefinementPacketCommand,
  ReviewRefinementPacketResult
> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: command.featureId,
      });
      const revision = await deps.repository.getLatest(feature);
      if (!revision || revision.id !== command.revisionId)
        throw new Error("The reviewed revision is no longer current.");
      await deps.repository.recordReview(
        revision,
        command.decision,
        command.feedback,
      );
      if (command.decision === "approved") {
        await deps.repository.updateStatus(revision, "approved");
        return { success: true, data: { approved: true } };
      }
      if (!command.feedback?.trim())
        throw new Error(
          "Explain what should change before requesting another architect pass.",
        );
      await deps.repository.updateStatus(revision, "changes_requested");
      const next = await deps.repository.create(feature, command.feedback);
      return {
        success: true,
        data: { approved: false, nextRevisionId: next.id },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "REVIEW_REFINEMENT_PACKET_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
