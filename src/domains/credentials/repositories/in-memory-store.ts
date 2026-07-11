import type { CredentialStore } from "../types/credential-store.js";

export class InMemoryCredentialStore implements CredentialStore {
  private store = new Map<string, Map<string, string>>();

  private profileKey(profile: string): string {
    return profile;
  }

  async get(profile: string, key: string): Promise<string | undefined> {
    return this.store.get(this.profileKey(profile))?.get(key);
  }

  async set(profile: string, key: string, value: string): Promise<void> {
    const pKey = this.profileKey(profile);
    if (!this.store.has(pKey)) {
      this.store.set(pKey, new Map());
    }
    this.store.get(pKey)!.set(key, value);
  }

  async delete(profile: string, key: string): Promise<void> {
    this.store.get(this.profileKey(profile))?.delete(key);
  }

  async list(profile: string): Promise<readonly string[]> {
    const entries = this.store.get(this.profileKey(profile));
    return entries ? [...entries.keys()] : [];
  }
}
