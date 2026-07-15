import type { SemanticVersion } from "../types/semantic-version.js";

const SEMVER_PATTERN =
  /^(?:v)?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

export function parseSemanticVersion(
  value: string,
): SemanticVersion | undefined {
  const match = SEMVER_PATTERN.exec(value);
  if (!match) return undefined;
  const prerelease = match[4]
    ? match[4]
        .split(".")
        .map((identifier) =>
          /^\d+$/.test(identifier) ? Number(identifier) : identifier,
        )
    : [];
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease,
  };
}

export function compareSemanticVersions(
  left: SemanticVersion,
  right: SemanticVersion,
): number {
  for (const key of ["major", "minor", "patch"] as const) {
    if (left[key] !== right[key]) return left[key] < right[key] ? -1 : 1;
  }
  if (left.prerelease.length === 0 && right.prerelease.length === 0) return 0;
  if (left.prerelease.length === 0) return 1;
  if (right.prerelease.length === 0) return -1;

  const length = Math.max(left.prerelease.length, right.prerelease.length);
  for (let index = 0; index < length; index += 1) {
    const leftPart = left.prerelease[index];
    const rightPart = right.prerelease[index];
    if (leftPart === undefined) return -1;
    if (rightPart === undefined) return 1;
    if (leftPart === rightPart) continue;
    if (typeof leftPart === "number" && typeof rightPart === "string")
      return -1;
    if (typeof leftPart === "string" && typeof rightPart === "number") return 1;
    return leftPart < rightPart ? -1 : 1;
  }
  return 0;
}

export function compareSemanticVersionStrings(
  left: string,
  right: string,
): number | undefined {
  const parsedLeft = parseSemanticVersion(left);
  const parsedRight = parseSemanticVersion(right);
  if (!parsedLeft || !parsedRight) return undefined;
  return compareSemanticVersions(parsedLeft, parsedRight);
}
