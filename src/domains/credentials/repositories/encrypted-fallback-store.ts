import {
  randomBytes,
  createCipheriv,
  createDecipheriv,
  scrypt,
} from "node:crypto";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";
import type { CredentialStore } from "../interfaces/credential-store.js";
import type { EncryptedPayload, VaultData } from "../types/encrypted-vault.js";
import {
  CredentialStoreError,
  EncryptionError,
} from "../errors/credential-errors.js";
import { pathExists } from "../../../config.js";

const scryptAsync = promisify(scrypt);

const ALGORITHM = "aes-256-gcm";
const KEY_LENGTH = 32;
const IV_LENGTH = 16;
const SALT_LENGTH = 32;

export class EncryptedFallbackStore implements CredentialStore {
  private vaultPath: string;
  private keyPath: string;
  private vault: VaultData | null = null;
  private key: Buffer | null = null;
  private initialized = false;

  constructor(globalConfigDir: string) {
    this.vaultPath = path.join(globalConfigDir, "credentials.vault");
    this.keyPath = path.join(globalConfigDir, "credentials.key");
  }

  async initialize(): Promise<void> {
    const dir = path.dirname(this.vaultPath);
    await mkdir(dir, { recursive: true });

    if (await pathExists(this.keyPath)) {
      const keyHex = await readFile(this.keyPath, "utf8");
      this.key = Buffer.from(keyHex.trim(), "hex");
    } else {
      this.key = randomBytes(KEY_LENGTH);
      await writeFile(this.keyPath, this.key.toString("hex"), {
        mode: 0o600,
      });
    }

    if (await pathExists(this.vaultPath)) {
      await this.loadVault();
    } else {
      this.vault = {
        version: 1,
        salt: randomBytes(SALT_LENGTH).toString("hex"),
        entries: {},
      };
      await this.saveVault();
    }
    this.initialized = true;
  }

  private ensureInitialized(): void {
    if (!this.initialized) {
      throw new CredentialStoreError(
        "Vault not initialized",
        "VAULT_NOT_INITIALIZED",
      );
    }
  }

  private async loadVault(): Promise<void> {
    try {
      const raw = await readFile(this.vaultPath, "utf8");
      const payload: EncryptedPayload = JSON.parse(raw);
      const decrypted = await this.decrypt(payload);
      this.vault = JSON.parse(decrypted);
    } catch (error) {
      throw new CredentialStoreError(
        "Failed to load vault",
        "VAULT_LOAD_ERROR",
        error,
      );
    }
  }

  private async saveVault(): Promise<void> {
    if (!this.vault || !this.key) {
      throw new CredentialStoreError(
        "Vault not initialized",
        "VAULT_NOT_INITIALIZED",
      );
    }
    try {
      const encrypted = await this.encrypt(JSON.stringify(this.vault));
      await writeFile(this.vaultPath, JSON.stringify(encrypted), {
        mode: 0o600,
      });
    } catch (error) {
      throw new CredentialStoreError(
        "Failed to save vault",
        "VAULT_SAVE_ERROR",
        error,
      );
    }
  }

  private async deriveKey(salt: Buffer): Promise<Buffer> {
    if (!this.key) throw new EncryptionError(new Error("Key not loaded"));
    return (await scryptAsync(this.key, salt, KEY_LENGTH)) as Buffer;
  }

  private async encrypt(plaintext: string): Promise<EncryptedPayload> {
    const iv = randomBytes(IV_LENGTH);
    const salt = randomBytes(SALT_LENGTH);
    const derivedKey = await this.deriveKey(salt);
    const cipher = createCipheriv(ALGORITHM, derivedKey, iv);
    const encrypted = Buffer.concat([
      cipher.update(plaintext, "utf8"),
      cipher.final(),
    ]);
    const tag = cipher.getAuthTag();
    return {
      iv: iv.toString("hex"),
      tag: tag.toString("hex"),
      data: Buffer.concat([salt, encrypted]).toString("hex"),
    };
  }

  private async decrypt(payload: EncryptedPayload): Promise<string> {
    const dataBuf = Buffer.from(payload.data, "hex");
    const salt = dataBuf.subarray(0, SALT_LENGTH);
    const encrypted = dataBuf.subarray(SALT_LENGTH);
    const derivedKey = await this.deriveKey(salt);
    const iv = Buffer.from(payload.iv, "hex");
    const tag = Buffer.from(payload.tag, "hex");
    const decipher = createDecipheriv(ALGORITHM, derivedKey, iv);
    decipher.setAuthTag(tag);
    const decrypted = Buffer.concat([
      decipher.update(encrypted),
      decipher.final(),
    ]);
    return decrypted.toString("utf8");
  }

  async get(profile: string, key: string): Promise<string | undefined> {
    this.ensureInitialized();
    return this.vault!.entries[profile]?.[key];
  }

  async set(profile: string, key: string, value: string): Promise<void> {
    this.ensureInitialized();
    const profileEntries = {
      ...(this.vault!.entries[profile] ?? {}),
      [key]: value,
    };
    this.vault = {
      ...this.vault!,
      entries: { ...this.vault!.entries, [profile]: profileEntries },
    };
    await this.saveVault();
  }

  async delete(profile: string, key: string): Promise<void> {
    this.ensureInitialized();
    const profileEntries = { ...(this.vault!.entries[profile] ?? {}) };
    delete profileEntries[key];
    this.vault = {
      ...this.vault!,
      entries: { ...this.vault!.entries, [profile]: profileEntries },
    };
    await this.saveVault();
  }

  async list(profile: string): Promise<readonly string[]> {
    this.ensureInitialized();
    return Object.keys(this.vault!.entries[profile] ?? {});
  }
}

export class CompositeCredentialStore implements CredentialStore {
  private primary: CredentialStore;
  private fallback: CredentialStore;
  private useFallback = false;
  private initialized = false;

  constructor(primary: CredentialStore, fallback: CredentialStore) {
    this.primary = primary;
    this.fallback = fallback;
  }

  async initialize(): Promise<void> {
    if (this.fallback.initialize) {
      await this.fallback.initialize();
    }
    try {
      if (this.primary.initialize) {
        await this.primary.initialize();
      }
      this.useFallback = false;
    } catch {
      this.useFallback = true;
    }
    this.initialized = true;
  }

  isUsingFallback(): boolean {
    return this.useFallback;
  }

  async get(profile: string, key: string): Promise<string | undefined> {
    if (!this.initialized) {
      throw new CredentialStoreError(
        "Store not initialized",
        "STORE_NOT_INITIALIZED",
      );
    }
    const store = this.useFallback ? this.fallback : this.primary;
    return store.get(profile, key);
  }

  async set(profile: string, key: string, value: string): Promise<void> {
    if (!this.initialized) {
      throw new CredentialStoreError(
        "Store not initialized",
        "STORE_NOT_INITIALIZED",
      );
    }
    const store = this.useFallback ? this.fallback : this.primary;
    return store.set(profile, key, value);
  }

  async delete(profile: string, key: string): Promise<void> {
    if (!this.initialized) {
      throw new CredentialStoreError(
        "Store not initialized",
        "STORE_NOT_INITIALIZED",
      );
    }
    const store = this.useFallback ? this.fallback : this.primary;
    return store.delete(profile, key);
  }

  async list(profile: string): Promise<readonly string[]> {
    if (!this.initialized) {
      throw new CredentialStoreError(
        "Store not initialized",
        "STORE_NOT_INITIALIZED",
      );
    }
    const store = this.useFallback ? this.fallback : this.primary;
    return store.list(profile);
  }
}
