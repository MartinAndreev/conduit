export class GlobalProfileError extends Error {
  constructor(
    readonly code: "SECRET_REJECTED" | "VERSION_CONFLICT" | "INVALID_PROFILE",
    message: string,
  ) {
    super(message);
    this.name = "GlobalProfileError";
  }
}
