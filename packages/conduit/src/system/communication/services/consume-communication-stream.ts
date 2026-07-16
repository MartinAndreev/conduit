import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "../types/runtime-event.js";

export async function consumeCommunicationStream(
  stream: AsyncGenerator<ConduitRuntimeEvent, NativeTerminalResult, void>,
  persist: (event: ConduitRuntimeEvent) => Promise<void>,
): Promise<NativeTerminalResult> {
  while (true) {
    const next = await stream.next();
    if (next.done === true) return next.value;
    await persist(next.value);
  }
}
