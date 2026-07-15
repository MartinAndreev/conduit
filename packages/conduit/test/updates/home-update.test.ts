import { test } from "bun:test";
import assert from "node:assert/strict";
import { InstallationKind } from "../../src/domains/updates/enums/installation-kind.js";
import { UpdateStatus } from "../../src/domains/updates/enums/update-status.js";
import type { AvailableUpdateStatus } from "../../src/domains/updates/types/update-status-read-model.js";
import {
  canOfferUpdate,
  canOpenUpdateConfirmation,
  decideUpdateConfirmationKey,
  homeInteractionReducer,
} from "../../src/tui/helpers/home-interaction.js";
import {
  updateConfirmationDetails,
  updateStatusLabel,
} from "../../src/tui/helpers/update-presentation.js";

const available: AvailableUpdateStatus = {
  schemaVersion: 1,
  status: UpdateStatus.Available,
  currentVersion: "0.5.4",
  targetVersion: "0.6.0",
  release: {
    version: "0.6.0",
    tagName: "v0.6.0",
    publishedAt: "2026-07-15T08:00:00Z",
    releaseUrl: "https://github.com/MartinAndreev/conduit/releases/tag/v0.6.0",
    assets: [],
  },
  installation: {
    kind: InstallationKind.Unknown,
    automatic: false,
    label: "Unknown installation (manual update only)",
  },
};

test("Home version labels distinguish every discovery state", () => {
  assert.equal(
    updateStatusLabel({
      schemaVersion: 1,
      status: UpdateStatus.Checking,
      currentVersion: "0.5.4",
    }),
    "checking",
  );
  assert.equal(
    updateStatusLabel({
      schemaVersion: 1,
      status: UpdateStatus.Current,
      currentVersion: "0.5.4",
      message: "Conduit is up to date.",
    }),
    "up to date",
  );
  assert.equal(updateStatusLabel(available), "v0.6.0 available");
  assert.equal(
    updateStatusLabel({
      schemaVersion: 1,
      status: UpdateStatus.Unavailable,
      currentVersion: "0.5.4",
      message: "Offline.",
      retryable: true,
    }),
    "update status unavailable",
  );
});

test("update confirmation names versions and method and defaults to cancel", () => {
  assert.deepEqual(updateConfirmationDetails(available), [
    "Current: v0.5.4",
    "Target:  v0.6.0",
    "Method:  Unknown installation (manual update only)",
  ]);
  assert.deepEqual(
    homeInteractionReducer(
      { kind: "idle" },
      { type: "openUpdateConfirmation" },
    ),
    { kind: "updateConfirmation", actionIndex: 0 },
  );
});

test("update action is available only while idle Home owns focus", () => {
  assert.equal(canOfferUpdate(available), true);
  assert.equal(canOpenUpdateConfirmation({ kind: "idle" }, available), true);
  assert.equal(
    canOpenUpdateConfirmation({ kind: "search", query: "agent" }, available),
    false,
  );
  assert.equal(
    canOpenUpdateConfirmation({ kind: "create", title: "feature" }, available),
    false,
  );
  assert.equal(
    canOpenUpdateConfirmation(
      { kind: "featureActions", actionIndex: 0 },
      available,
    ),
    false,
  );
  assert.equal(
    canOpenUpdateConfirmation(
      { kind: "updateConfirmation", actionIndex: 0 },
      available,
    ),
    false,
  );
});

test("confirmation keyboard behavior cancels by default and requires explicit selection", () => {
  const defaultConfirmation = {
    kind: "updateConfirmation" as const,
    actionIndex: 0 as const,
  };
  assert.deepEqual(decideUpdateConfirmationKey(defaultConfirmation, "return"), {
    kind: "interaction",
    action: { type: "idle" },
  });
  assert.deepEqual(decideUpdateConfirmationKey(defaultConfirmation, "escape"), {
    kind: "interaction",
    action: { type: "idle" },
  });
  assert.deepEqual(decideUpdateConfirmationKey(defaultConfirmation, "q"), {
    kind: "interaction",
    action: { type: "idle" },
  });
  assert.deepEqual(decideUpdateConfirmationKey(defaultConfirmation, "right"), {
    kind: "interaction",
    action: { type: "selectUpdateAction", value: 1 },
  });
  assert.deepEqual(
    decideUpdateConfirmationKey(
      { kind: "updateConfirmation", actionIndex: 1 },
      "return",
    ),
    { kind: "startUpdate" },
  );
});
