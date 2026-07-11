export interface CredentialStore {
  initialize?(): Promise<void>;
  get(profile: string, key: string): Promise<string | undefined>;
  set(profile: string, key: string, value: string): Promise<void>;
  delete(profile: string, key: string): Promise<void>;
  list(profile: string): Promise<readonly string[]>;
}

export interface CredentialEntry {
  readonly profile: string;
  readonly key: string;
}

export const CREDENTIAL_STORE_VERSION = 1;
