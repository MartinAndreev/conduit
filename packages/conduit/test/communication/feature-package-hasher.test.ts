import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { hashFeaturePackage } from "../../src/domains/features/services/feature-package-hasher.js";

test("feature package hash is stable and ignores runtime state", async () => {
  await withPackage(async (root) => {
    await writePackage(root, { spec: "# Spec\n", contract: '{"a":1}\n' });
    const first = await hashFeaturePackage({ packageRoot: root });
    await mkdir(path.join(root, ".conduit"));
    await writeFile(path.join(root, ".conduit", "events.json"), "runtime");
    await writeFile(path.join(root, "transcript.jsonl"), "noise");
    const second = await hashFeaturePackage({ packageRoot: root });
    assert.equal(second.hash, first.hash);
    assert.deepEqual(second.files, first.files);
  });
});

test("feature package hash normalizes line endings", async () => {
  await withPackage(async (root) => {
    await writePackage(root, { spec: "# Spec\r\nA\r", contract: "x\r\n" });
    const crlf = await hashFeaturePackage({ packageRoot: root });
    await writePackage(root, { spec: "# Spec\nA\n", contract: "x\n" });
    const lf = await hashFeaturePackage({ packageRoot: root });
    assert.equal(crlf.hash, lf.hash);
    assert.equal(lf.lineEndings, "lf-normalized");
  });
});

test("feature package hash changes for material approved package changes", async () => {
  await withPackage(async (root) => {
    await writePackage(root, { spec: "# Spec\nA\n", contract: "x\n" });
    const first = await hashFeaturePackage({ packageRoot: root });
    await writePackage(root, { spec: "# Spec\nB\n", contract: "x\n" });
    const second = await hashFeaturePackage({ packageRoot: root });
    assert.notEqual(second.hash, first.hash);
  });
});

test("feature package hash includes normalized role ownership inputs", async () => {
  await withPackage(async (root) => {
    await writePackage(root, { spec: "# Spec\n", contract: "x\n" });
    const first = await hashFeaturePackage({
      packageRoot: root,
      ownershipInputs: [
        {
          role: "worker",
          readOnly: false,
          owns: ["b", "a"],
          dependsOn: ["qa"],
        },
      ],
    });
    const reordered = await hashFeaturePackage({
      packageRoot: root,
      ownershipInputs: [
        {
          role: "worker",
          readOnly: false,
          owns: ["a", "b"],
          dependsOn: ["qa"],
        },
      ],
    });
    const changed = await hashFeaturePackage({
      packageRoot: root,
      ownershipInputs: [
        {
          role: "worker",
          readOnly: false,
          owns: ["a", "c"],
          dependsOn: ["qa"],
        },
      ],
    });
    assert.equal(reordered.hash, first.hash);
    assert.notEqual(changed.hash, first.hash);
  });
});

async function withPackage(
  work: (root: string) => Promise<void>,
): Promise<void> {
  const root = await mkdtemp(path.join(tmpdir(), "conduit-package-"));
  try {
    await work(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

async function writePackage(
  root: string,
  content: { readonly spec: string; readonly contract: string },
): Promise<void> {
  await mkdir(path.join(root, "contracts"), { recursive: true });
  await writeFile(path.join(root, "spec.md"), content.spec);
  await writeFile(
    path.join(root, "contracts", "contract.json"),
    content.contract,
  );
}
