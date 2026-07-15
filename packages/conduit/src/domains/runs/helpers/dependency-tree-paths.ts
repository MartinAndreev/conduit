const dependencyTreeRoots = [
  "node_modules",
  ".pnpm",
  "vendor",
  ".venv",
  "venv",
  ".yarn/cache",
  ".yarn/unplugged",
] as const;

const untrackedGeneratedOutputRoots = [
  "dist",
  "build",
  "out",
  "coverage",
  "test-results",
  "playwright-report",
  ".nyc_output",
  ".vite",
] as const;

function gitExcludesFor(roots: readonly string[]): string[] {
  return roots.flatMap((root) => [
    `--exclude=${root}/**`,
    `--exclude=**/${root}/**`,
  ]);
}

function isWithinRoot(filePath: string, root: string): boolean {
  return (
    filePath === root ||
    filePath.startsWith(`${root}/`) ||
    filePath.includes(`/${root}/`)
  );
}

export function dependencyTreeGitExcludes(): string[] {
  return gitExcludesFor(dependencyTreeRoots);
}

export function isDependencyTreePath(filePath: string): boolean {
  return dependencyTreeRoots.some((root) => isWithinRoot(filePath, root));
}

export function untrackedArtifactGitExcludes(): string[] {
  return gitExcludesFor([
    ...dependencyTreeRoots,
    ...untrackedGeneratedOutputRoots,
  ]);
}

export function isUntrackedArtifactPath(filePath: string): boolean {
  return (
    isDependencyTreePath(filePath) ||
    untrackedGeneratedOutputRoots.some((root) => isWithinRoot(filePath, root))
  );
}
