export interface NavigationItem {
  readonly label: string;
  readonly path: string;
  readonly external?: boolean;
}

export const repositoryUrl = "https://github.com/MartinAndreev/conduit";

export const navigationItems: readonly NavigationItem[] = [
  { label: "Home", path: "" },
  { label: "Docs", path: "docs/" },
  { label: "Releases", path: "releases/" },
  { label: "Roadmap", path: "roadmap/" },
  { label: "GitHub", path: repositoryUrl, external: true },
];

export function withBase(base: string, path: string): string {
  if (/^https?:\/\//u.test(path)) return path;
  return `${base}${path}`;
}

export function isActiveNavigationPath(
  currentPath: string,
  item: NavigationItem,
): boolean {
  if (item.external) return false;
  if (item.path === "") return currentPath === "/";
  return currentPath.startsWith(`/${item.path}`);
}
