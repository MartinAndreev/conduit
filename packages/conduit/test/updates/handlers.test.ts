import { test } from "bun:test";
import assert from "node:assert/strict";
import { createCheckForUpdateHandler } from "../../src/domains/updates/handlers/check-for-update-handler.js";
import { UpdateDiscoveryError } from "../../src/domains/updates/errors/update-errors.js";
import { UpdateStatus } from "../../src/domains/updates/enums/update-status.js";
import type { ReleaseDiscovery } from "../../src/domains/updates/interfaces/release-discovery.js";

test("check handler maps newer, current, and unavailable results to versioned read models", async () => {
  const availableDiscovery: ReleaseDiscovery = {
    discover: async () => ({
      version: "2.0.0",
      tagName: "v2.0.0",
      publishedAt: "2026-07-15T08:00:00Z",
      releaseUrl:
        "https://github.com/MartinAndreev/conduit/releases/tag/v2.0.0",
      assets: [],
    }),
  };
  const available = await createCheckForUpdateHandler(
    availableDiscovery,
    "1.0.0",
  )({ type: "checkForUpdate" });
  assert.equal(available.success, true);
  if (available.success) {
    assert.equal(available.data.schemaVersion, 1);
    assert.equal(available.data.status, UpdateStatus.Available);
    assert.equal(available.data.targetVersion, "2.0.0");
  }

  const current = await createCheckForUpdateHandler(
    { discover: async () => undefined },
    "2.0.0",
  )({ type: "checkForUpdate" });
  assert.equal(current.success && current.data.status, UpdateStatus.Current);

  const unavailable = await createCheckForUpdateHandler(
    {
      discover: async () => {
        throw new UpdateDiscoveryError(
          "OFFLINE",
          "The release service could not be reached.",
        );
      },
    },
    "2.0.0",
  )({ type: "checkForUpdate" });
  assert.equal(
    unavailable.success && unavailable.data.status,
    UpdateStatus.Unavailable,
  );
  if (
    unavailable.success &&
    unavailable.data.status === UpdateStatus.Unavailable
  )
    assert.equal(unavailable.data.message?.includes("response body"), false);
});
