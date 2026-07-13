import type { Command } from "../../../../system/bus/command-bus.js";

export interface CreateFeatureCommand extends Command {
  readonly type: "createFeature";
  readonly title: string;
}
export interface CreateFeatureResult {
  readonly id: string;
  readonly directory: string;
}
