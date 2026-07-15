import { test } from "bun:test";
import assert from "node:assert/strict";
import {
  compareSemanticVersionStrings,
  parseSemanticVersion,
} from "../../src/domains/updates/helpers/semver.js";

test("strict SemVer parsing accepts stable tags and rejects malformed versions", () => {
  assert.deepEqual(parseSemanticVersion("v1.20.3+build.7"), {
    major: 1,
    minor: 20,
    patch: 3,
    prerelease: [],
  });
  assert.equal(parseSemanticVersion("1.02.3"), undefined);
  assert.equal(parseSemanticVersion("release-1.2.3"), undefined);
  assert.equal(parseSemanticVersion("1.2"), undefined);
});

test("SemVer comparison is numeric and applies prerelease precedence", () => {
  assert.equal(compareSemanticVersionStrings("1.10.0", "1.9.0"), 1);
  assert.equal(compareSemanticVersionStrings("2.0.0", "10.0.0"), -1);
  assert.equal(compareSemanticVersionStrings("1.0.0", "1.0.0-rc.1"), 1);
  assert.equal(compareSemanticVersionStrings("1.0.0-rc.10", "1.0.0-rc.2"), 1);
  assert.equal(compareSemanticVersionStrings("invalid", "1.0.0"), undefined);
});
