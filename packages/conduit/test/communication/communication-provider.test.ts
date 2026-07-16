import assert from "node:assert/strict";
import test from "node:test";
import { CommunicationProviderId } from "../../src/system/communication/enums/communication-provider-id.js";
import {
  candidateCommunicationProviders,
  createDefaultCommunicationProviders,
} from "../../src/system/communication/services/provider-registry.js";
import { consumeCommunicationStream } from "../../src/system/communication/services/consume-communication-stream.js";
import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "../../src/system/communication/types/runtime-event.js";

test("default communication registry contains separate preferred and fallback providers", () => {
  const providers = createDefaultCommunicationProviders();
  assert.deepEqual(
    providers.map((provider) => provider.id),
    [
      CommunicationProviderId.CodexAppServer,
      CommunicationProviderId.CodexExec,
      CommunicationProviderId.OpenCodeAcp,
      CommunicationProviderId.OpenCodeJson,
      CommunicationProviderId.PiRpc,
      CommunicationProviderId.PiJson,
      CommunicationProviderId.KiloAcp,
      CommunicationProviderId.KiloJson,
    ],
  );
});

test("candidate provider selection is external to provider implementation", () => {
  const providers = createDefaultCommunicationProviders();
  assert.deepEqual(
    candidateCommunicationProviders(providers, "opencode").map(
      (provider) => provider.id,
    ),
    [CommunicationProviderId.OpenCodeAcp, CommunicationProviderId.OpenCodeJson],
  );
});

test("communication stream consumer persists in order and returns terminal result", async () => {
  const persisted: number[] = [];
  const terminal = await consumeCommunicationStream(fakeStream(), async (event) => {
    persisted.push(event.sequence);
  });

  assert.deepEqual(persisted, [1, 2]);
  assert.equal(terminal.status, "completed");
  assert.equal(terminal.finalResponseCandidate, "{}");
});

test("communication stream consumer awaits persistence before next event", async () => {
  const checkpoints: string[] = [];
  await consumeCommunicationStream(observedStream(checkpoints), async (event) => {
    checkpoints.push(`persist-start-${event.sequence}`);
    await Promise.resolve();
    checkpoints.push(`persist-end-${event.sequence}`);
  });

  assert.deepEqual(checkpoints, [
    "yield-1",
    "persist-start-1",
    "persist-end-1",
    "yield-2",
    "persist-start-2",
    "persist-end-2",
  ]);
});

async function* fakeStream(): AsyncGenerator<
  ConduitRuntimeEvent,
  NativeTerminalResult,
  void
> {
  yield event(1);
  yield event(2);
  return { status: "completed", finalResponseCandidate: "{}", diagnostics: [] };
}

async function* observedStream(
  checkpoints: string[],
): AsyncGenerator<ConduitRuntimeEvent, NativeTerminalResult, void> {
  checkpoints.push("yield-1");
  yield event(1);
  checkpoints.push("yield-2");
  yield event(2);
  return { status: "completed", diagnostics: [] };
}

function event(sequence: number): ConduitRuntimeEvent {
  return {
    version: "1.0",
    sequence,
    receivedAt: "2026-07-16T00:00:00.000Z",
    context: { runId: "run-1", roleId: "role-1" },
    provenance: "runner-reported",
    type: "agent-activity",
    payload: { message: `event ${sequence}` },
  };
}
