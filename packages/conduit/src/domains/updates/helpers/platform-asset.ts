export function releaseAssetName(
  platform: NodeJS.Platform,
  architecture: string,
): string | undefined {
  if (platform === "linux" && architecture === "x64")
    return "conduit-linux-x64";
  if (platform === "linux" && architecture === "arm64")
    return "conduit-linux-arm64";
  if (platform === "darwin" && architecture === "arm64")
    return "conduit-darwin-arm64";
  if (platform === "win32" && architecture === "x64")
    return "conduit-windows-x64.exe";
  return undefined;
}
