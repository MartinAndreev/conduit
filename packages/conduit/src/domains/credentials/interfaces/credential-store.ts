export interface CredentialStore {
  initialize?(): Promise<void>;
  isAvailable?(): boolean;
  get(profile: string, key: string): Promise<string | undefined>;
  set(profile: string, key: string, value: string): Promise<void>;
  delete(profile: string, key: string): Promise<void>;
  list(profile: string): Promise<readonly string[]>;
}
