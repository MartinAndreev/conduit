import type { Command } from "@system/bus/command-bus.js";

export interface ReviewRefinementPacketCommand extends Command {
  readonly type: "reviewRefinementPacket";
  readonly featureId: string;
  readonly revisionId: string;
  readonly decision: "approved" | "changes_requested";
  readonly feedback?: string;
}

export interface ReviewRefinementPacketResult {
  readonly approved: boolean;
  readonly nextRevisionId?: string;
}
