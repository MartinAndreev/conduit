import type { RunnerEvent } from "../../../domains/runs/types/runner-events.js";

export interface RunnerOutputParser {
  push(chunk: string): readonly RunnerEvent[];
  flush(): readonly RunnerEvent[];
  readonly finalResponse: string | undefined;
}
