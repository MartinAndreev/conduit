import type { CredentialStore } from "../types/credential-store.js";
import { VaultUnavailableError } from "../errors/credential-errors.js";

interface KeytarModule {
  getPassword(service: string, account: string): Promise<string | null>;
  setPassword(
    service: string,
    account: string,
    password: string,
  ): Promise<void>;
  deletePassword(service: string, account: string): Promise<boolean>;
  findCredentials(
    service: string,
  ): Promise<Array<{ account: string; password: string }>>;
}

const SERVICE_NAME = "conduit-orchestrator";

export class OSVaultStore implements CredentialStore {
  private keytar: KeytarModule | null = null;
  private initialized = false;
  private available = false;

  async initialize(): Promise<void> {
    try {
      const mod = await import("keytar");
      this.keytar = mod.default as KeytarModule;
      await this.keytar.getPassword(SERVICE_NAME, "__probe__");
      this.available = true;
    } catch {
      this.available = false;
      this.keytar = null;
    }
    this.initialized = true;
  }

  isAvailable(): boolean {
    return this.available;
  }

  private ensureInitialized(): void {
    if (!this.initialized) {
      throw new VaultUnavailableError(new Error("OS vault not initialized"));
    }
  }

  async get(profile: string, key: string): Promise<string | undefined> {
    this.ensureInitialized();
    if (!this.keytar) throw new VaultUnavailableError();
    const value = await this.keytar.getPassword(
      SERVICE_NAME,
      `${profile}:${key}`,
    );
    return value ?? undefined;
  }

  async set(profile: string, key: string, value: string): Promise<void> {
    this.ensureInitialized();
    if (!this.keytar) throw new VaultUnavailableError();
    await this.keytar.setPassword(SERVICE_NAME, `${profile}:${key}`, value);
  }

  async delete(profile: string, key: string): Promise<void> {
    this.ensureInitialized();
    if (!this.keytar) throw new VaultUnavailableError();
    await this.keytar.deletePassword(SERVICE_NAME, `${profile}:${key}`);
  }

  async list(profile: string): Promise<readonly string[]> {
    this.ensureInitialized();
    if (!this.keytar) throw new VaultUnavailableError();
    const creds = await this.keytar.findCredentials(SERVICE_NAME);
    return creds
      .filter((c) => c.account.startsWith(`${profile}:`))
      .map((c) => c.account.slice(profile.length + 1));
  }
}
