export const refinementPacketReviewCopy = {
  heading: "Architect packet review",
  approveControl: "[a] Approve packet",
  requestChangesControl: "[r] Request changes",
  exitControl: "[q/Esc] Return home",
  emptyPacket: "# Packet\n\nNo packet files were produced.",
  feedbackPlaceholder:
    "Explain what is incorrect or missing. The architect will preserve approved decisions.",
} as const;

export function refinementFeedbackInstructions(submitKeyLabel: string): string {
  return `Describe what must change, then ${submitKeyLabel} sends the packet back to the architect.`;
}
