import type { Theme } from "@tui/theme.js";

export interface RefinementPacketReviewContent {
  readonly spec: string;
  readonly plan: string;
  readonly tasks: string;
  readonly testCases: string;
}

export interface RefinementPacketReviewProps {
  readonly theme: Theme;
  readonly content: RefinementPacketReviewContent;
  readonly onApprove: () => void;
  readonly onRequestChanges: (feedback: string) => void;
  readonly onExit: () => void;
}
