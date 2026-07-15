import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TursoGlobalProfileRepository } from "../src/domains/configuration/repositories/turso-global-profile-repository.js";
import { TursoGlobalConfigurationMetadataRepository } from "../src/domains/configuration/repositories/turso-global-configuration-metadata-repository.js";
import { GlobalDatabaseFactory } from "../src/system/storage/factories/database-factories.js";

test("global profiles persist partial role defaults with optimistic versions", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-profile-"));
  const repository = new TursoGlobalProfileRepository(
    new GlobalDatabaseFactory({ XDG_DATA_HOME: directory }),
  );
  try {
    const created = await repository.save({
      name: "default",
      runner: "codex",
      readOnly: true,
      owns: ["docs"],
    });
    assert.equal(created.version, 1);
    assert.equal(created.model, undefined);
    assert.deepEqual((await repository.load("default"))?.owns, ["docs"]);

    const updated = await repository.save({
      name: "default",
      runner: "opencode",
      expectedVersion: 1,
    });
    assert.equal(updated.version, 2);
    assert.deepEqual(updated.owns, ["docs"]);
    await assert.rejects(
      () =>
        repository.save({
          name: "default",
          runner: "codex",
          expectedVersion: 1,
        }),
      /updated by another operation/,
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("global profiles reject secret-like persisted fields", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-profile-secret-"));
  const repository = new TursoGlobalProfileRepository(
    new GlobalDatabaseFactory({ XDG_DATA_HOME: directory }),
  );
  try {
    await assert.rejects(
      () =>
        repository.save({
          name: "default",
          metadata: { password: "seeded-profile-secret" },
        }),
      /cannot contain credentials/,
    );
    const bytes = await readFile(join(directory, "conduit", "global.db")).catch(
      () => Buffer.alloc(0),
    );
    assert.equal(bytes.includes("seeded-profile-secret"), false);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("global configuration metadata persists the credential protection mode", async () => {
  const directory = await mkdtemp(join(tmpdir(), "conduit-global-metadata-"));
  const repository = new TursoGlobalConfigurationMetadataRepository(
    new GlobalDatabaseFactory({ XDG_DATA_HOME: directory }),
  );
  try {
    await repository.set("credentialProtection", {
      mode: "obfuscation-at-rest",
    });

    assert.deepEqual(await repository.get("credentialProtection"), {
      mode: "obfuscation-at-rest",
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
