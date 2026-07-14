export class DraftVersionConflictError extends Error {
  constructor(readonly featureId: string) {
    super(`Draft ${featureId} was updated by another operation.`);
    this.name = "DraftVersionConflictError";
  }
}
