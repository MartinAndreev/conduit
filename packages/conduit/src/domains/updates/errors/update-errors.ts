import { UpdateErrorKind } from "../enums/update-error-kind.js";

export class UpdateError extends Error {
  constructor(
    readonly kind: UpdateErrorKind,
    readonly code: string,
    message: string,
    readonly retryable: boolean,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "UpdateError";
  }
}

export class UpdateDiscoveryError extends UpdateError {
  constructor(
    code: string,
    message: string,
    retryable = true,
    cause?: unknown,
  ) {
    super(UpdateErrorKind.Discovery, code, message, retryable, { cause });
    this.name = "UpdateDiscoveryError";
  }
}

export class UpdateValidationError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Validation, code, message, false, { cause });
    this.name = "UpdateValidationError";
  }
}

export class UpdateIntegrityError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Integrity, code, message, false, { cause });
    this.name = "UpdateIntegrityError";
  }
}

export class UpdatePermissionError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Permission, code, message, false, { cause });
    this.name = "UpdatePermissionError";
  }
}

export class UpdatePlatformError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Platform, code, message, false, { cause });
    this.name = "UpdatePlatformError";
  }
}

export class UpdateProcessError extends UpdateError {
  constructor(
    code: string,
    message: string,
    retryable = true,
    cause?: unknown,
  ) {
    super(UpdateErrorKind.Process, code, message, retryable, { cause });
    this.name = "UpdateProcessError";
  }
}

export class UpdateReplacementError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Replacement, code, message, false, { cause });
    this.name = "UpdateReplacementError";
  }
}

export class UpdateRecoveryError extends UpdateError {
  constructor(code: string, message: string, cause?: unknown) {
    super(UpdateErrorKind.Recovery, code, message, false, { cause });
    this.name = "UpdateRecoveryError";
  }
}
