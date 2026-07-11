import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  resolveSkill,
  sha256,
} from "../src/domains/roles/repositories/skill-resolver.js";

test("sha256 is stable for pinned remote skills", () => {
  assert.equal(
    sha256("conduit"),
    "41a01f9945b8b394953d4d44a1522eb9c7654c7fc86a572b71fc63408e854440",
  );
});

test("a project role can replace a built-in skill with a local file", async () => {
  const projectRoot = await mkdtemp(path.join(tmpdir(), "conduit-test-"));
  await writeFile(
    path.join(projectRoot, "frontend.md"),
    "# Project frontend role\n",
  );
  const skill = await resolveSkill({
    projectRoot,
    roleName: "frontend",
    role: {
      runner: "opencode",
      mode: "subagent",
      skill: { source: "file:frontend.md" },
    },
    builtinRoot: path.join(projectRoot, "unused"),
  });
  assert.equal(skill.content, "# Project frontend role\n");
  assert.equal(skill.verified, true);
});
