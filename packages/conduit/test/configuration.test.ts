import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createConfigurationRepository } from "../src/domains/configuration/repositories/configuration-repository.js";
import type { GlobalProfileRepository } from "../src/domains/configuration/interfaces/global-profile-repository.js";
import type { GlobalProfile } from "../src/domains/configuration/types/global-profile.js";

test("configuration resolves global profile, project role, guidance, and provenance", async () => {
  const projectRoot = await mkdtemp(join(tmpdir(), "conduit-config-"));
  const profile: GlobalProfile = {
    name: "default",
    runner: "codex",
    model: "global-model",
    effort: "high",
    mode: "subagent",
    readOnly: true,
    owns: ["global"],
    skillSource: "file:.conduit/roles/default.md",
    metadata: {},
    version: 1,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
  const profiles: GlobalProfileRepository = {
    load: async () => profile,
    list: async () => [profile],
    save: async () => profile,
    delete: async () => true,
  };
  try {
    await mkdir(join(projectRoot, ".conduit", "roles"), { recursive: true });
    await writeFile(
      join(projectRoot, ".conduit", "roles", "backend.md"),
      "Use repository conventions. token=guidance-secret",
    );
    await writeFile(
      join(projectRoot, "conduit.yml"),
      [
        "version: 1",
        "specsDir: product-specs",
        "stateDir: .state",
        "roles:",
        "  backend:",
        "    runner: opencode",
        "    mode: primary",
        "    owns: [apps/api]",
        "    skill:",
        "      source: file:.conduit/roles/backend.md",
        "  qa:",
        "    model: project-model",
        "    skill:",
      ].join("\n"),
    );
    const result =
      await createConfigurationRepository(profiles).resolveSettings(
        projectRoot,
      );
    assert.equal(result.roles.backend?.runner, "opencode");
    assert.equal(result.roles.backend?.model, "global-model");
    assert.deepEqual(result.roles.backend?.owns, ["apps/api"]);
    assert.equal(
      result.roles.backend?.guidance?.includes("guidance-secret"),
      false,
    );
    assert.equal(result.provenance["roles.backend.runner"], "project");
    assert.equal(result.provenance["roles.backend.model"], "global-profile");
    assert.equal(result.provenance["roles.backend.guidance"], "role-guidance");
    assert.equal(result.roles.qa?.runner, "codex");
    assert.equal(result.roles.qa?.model, "project-model");
    assert.deepEqual(result.roles.qa?.owns, ["global"]);
    assert.equal(result.provenance["roles.qa.runner"], "global-profile");
  } finally {
    await rm(projectRoot, { recursive: true, force: true });
  }
});
