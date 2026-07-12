export class RefinementError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly cause?: unknown,
  ) {
    super(message);
    this.name = "RefinementError";
  }
}

export class DraftNotFoundError extends RefinementError {
  constructor(featureId: string) {
    super(`Draft not found for feature: ${featureId}`, "DRAFT_NOT_FOUND");
    this.name = "DraftNotFoundError";
  }
}

export class DraftSaveError extends RefinementError {
  constructor(featureId: string, cause?: unknown) {
    super(
      `Failed to save draft for feature: ${featureId}`,
      "DRAFT_SAVE_ERROR",
      cause,
    );
    this.name = "DraftSaveError";
  }
}

export class DraftDiscardError extends RefinementError {
  constructor(featureId: string, cause?: unknown) {
    super(
      `Failed to discard draft for feature: ${featureId}`,
      "DRAFT_DISCARD_ERROR",
      cause,
    );
    this.name = "DraftDiscardError";
  }
}
