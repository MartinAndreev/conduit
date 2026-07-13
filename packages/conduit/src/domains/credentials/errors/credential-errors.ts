export class CredentialStoreError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly cause?: unknown,
  ) {
    super(message);
    this.name = "CredentialStoreError";
  }
}

export class VaultUnavailableError extends CredentialStoreError {
  constructor(cause?: unknown) {
    super("OS credential vault is unavailable", "VAULT_UNAVAILABLE", cause);
    this.name = "VaultUnavailableError";
  }
}

export class CredentialNotFoundError extends CredentialStoreError {
  constructor(profile: string, key: string) {
    super(`Credential not found: ${profile}/${key}`, "CREDENTIAL_NOT_FOUND");
    this.name = "CredentialNotFoundError";
  }
}

export class EncryptionError extends CredentialStoreError {
  constructor(cause?: unknown) {
    super("Encryption operation failed", "ENCRYPTION_ERROR", cause);
    this.name = "EncryptionError";
  }
}
