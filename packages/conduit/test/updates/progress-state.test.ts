import { test } from "bun:test";
import assert from "node:assert/strict";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { UpdateProgressStage } from "../../src/domains/updates/enums/update-progress-stage.js";
import { UpdateStatus } from "../../src/domains/updates/enums/update-status.js";
import { createStartUpdateHandler } from "../../src/domains/updates/handlers/start-update-handler.js";
import type { UpdateInstaller } from "../../src/domains/updates/interfaces/update-installer.js";
import type { UpdateStatusRepository } from "../../src/domains/updates/interfaces/update-status-repository.js";
import type { UpdateStatusReadModel } from "../../src/domains/updates/types/update-status-read-model.js";

const command = {
  type: "startUpdate" as const,
  release: {
    version: "0.6.0",
    tagName: "v0.6.0",
    publishedAt: "2026-07-15T08:00:00Z",
    releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
    assets: [],
  },
  installation: {
    kind: InstallationKind.GlobalPackage,
    automatic: true,
    label: "pnpm global package",
    packageManager: "pnpm" as const,
  },
};

test("start command publishes typed progress and terminal success", async () => {
  const history: UpdateStatusReadModel[] = [];
  const repository: UpdateStatusRepository = {
    get: () => history.at(-1)!,
    set: (status) => history.push(status),
  };
  const installer: UpdateInstaller = {
    install: async (_request, onProgress) => {
      for (const stage of [
        UpdateProgressStage.Preparing,
        UpdateProgressStage.Downloading,
        UpdateProgressStage.Verifying,
        UpdateProgressStage.Installing,
        UpdateProgressStage.Complete,
      ])
        onProgress({ stage, message: stage });
    },
  };
  const result = await createStartUpdateHandler(
    installer,
    "0.5.4",
    repository,
  )(command);
  assert.equal(result.success, true);
  assert.deepEqual(
    history
      .filter((status) => status.status === UpdateStatus.Updating)
      .map((status) => status.progress.stage),
    [
      UpdateProgressStage.Preparing,
      UpdateProgressStage.Preparing,
      UpdateProgressStage.Downloading,
      UpdateProgressStage.Verifying,
      UpdateProgressStage.Installing,
      UpdateProgressStage.Complete,
    ],
  );
  assert.equal(history.at(-1)?.status, UpdateStatus.Succeeded);
});

test("start command rejects equal or older target versions before installation", async () => {
  let installed = false;
  const installer: UpdateInstaller = {
    install: async () => {
      installed = true;
    },
  };
  const result = await createStartUpdateHandler(
    installer,
    "0.6.0",
  )({
    ...command,
    release: { ...command.release, version: "0.5.4", tagName: "v0.5.4" },
  });
  assert.equal(installed, false);
  assert.equal(result.success && result.data.status, UpdateStatus.Failed);
});
