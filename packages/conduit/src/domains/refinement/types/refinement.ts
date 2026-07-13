import type { Feature } from "../../features/types/feature.js";

export interface RefinementResult {
  feature: Feature;
  storyFile?: string;
  testCasesFile?: string;
  architectRan: boolean;
  logFile?: string;
}
