export interface VaultData {
  readonly version: number;
  readonly salt: string;
  readonly entries: Record<string, Record<string, string>>;
}

export interface EncryptedPayload {
  readonly iv: string;
  readonly tag: string;
  readonly data: string;
}
