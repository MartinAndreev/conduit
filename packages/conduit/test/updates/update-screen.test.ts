import { test } from "bun:test";
import assert from "node:assert/strict";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { UpdateProgressStage } from "../../src/domains/updates/enums/update-progress-stage.js";
import { UpdateStatus } from "../../src/domains/updates/enums/update-status.js";
import type {
  FailedUpdateStatus,
  SucceededUpdateStatus,
  UpdatingUpdateStatus,
} from "../../src/domains/updates/types/update-status-read-model.js";
import {
  isUpdateAnimating,
  updateScreenActions,
  updateScreenKeyAction,
  updateSuccessGuidance,
} from "../../src/tui/helpers/update-screen-presentation.js";

const release = {
  version: "0.6.0",
  tagName: "v0.6.0",
  publishedAt: "2026-07-15T08:00:00Z",
  releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
  assets: [],
};
const packageInstallation = {
  kind: InstallationKind.GlobalPackage,
  automatic: true,
  label: "pnpm global package",
  packageManager: "pnpm" as const,
};
const base = {
  schemaVersion: 1 as const,
  currentVersion: "0.5.4",
  targetVersion: "0.6.0",
  release,
  installation: packageInstallation,
};

test("loader runs only for non-terminal update state", () => {
  const updating: UpdatingUpdateStatus = {
    ...base,
    status: UpdateStatus.Updating,
    progress: {
      stage: UpdateProgressStage.Installing,
      message: "Installing",
    },
  };
  const succeeded: SucceededUpdateStatus = {
    ...base,
    status: UpdateStatus.Succeeded,
    progress: { stage: UpdateProgressStage.Complete, message: "Complete" },
  };
  const failed: FailedUpdateStatus = {
    ...base,
    status: UpdateStatus.Failed,
    progress: { stage: UpdateProgressStage.Verifying, message: "Verifying" },
    message: "Checksum mismatch.",
    retryable: true,
  };
  assert.equal(isUpdateAnimating(updating), true);
  assert.equal(isUpdateAnimating(succeeded), false);
  assert.equal(isUpdateAnimating(failed), false);
  assert.equal(updateScreenKeyAction(updating, "q"), "none");
  assert.equal(updateScreenKeyAction(failed, "r"), "retry");
  assert.equal(updateScreenKeyAction(failed, "escape"), "home");
  assert.equal(updateScreenKeyAction(succeeded, "q"), "quit");
  assert.match(updateScreenActions(failed), /Retry/);
  assert.match(
    updateSuccessGuidance(succeeded)[0] ?? "",
    /next Conduit launch/,
  );
});

test("standalone and manual success guidance is strategy-specific", () => {
  const standalone: SucceededUpdateStatus = {
    ...base,
    status: UpdateStatus.Succeeded,
    installation: {
      kind: InstallationKind.Standalone,
      automatic: true,
      label: "Official standalone binary",
      executablePath: "/tmp/conduit",
      assetName: "conduit-linux-x64",
    },
    progress: { stage: UpdateProgressStage.Complete, message: "Complete" },
  };
  const guided: SucceededUpdateStatus = {
    ...base,
    status: UpdateStatus.Succeeded,
    installation: {
      kind: InstallationKind.Unknown,
      automatic: false,
      label: "Manual update",
      manualUrl: "https://github.com/MartinAndreev/conduit/releases/latest",
    },
    progress: { stage: UpdateProgressStage.Complete, message: "Complete" },
  };
  assert.match(updateSuccessGuidance(standalone).join(" "), /old version/i);
  assert.deepEqual(updateSuccessGuidance(guided), [
    "https://github.com/MartinAndreev/conduit/releases/latest",
  ]);
});
