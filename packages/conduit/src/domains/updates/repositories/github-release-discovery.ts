import { UpdateDiscoveryError } from "../errors/update-errors.js";
import {
  compareSemanticVersions,
  parseSemanticVersion,
} from "../helpers/semver.js";
import type { ReleaseDiscovery } from "../interfaces/release-discovery.js";
import type {
  ReleaseDiscoveryOptions,
  ReleaseFetch,
} from "../types/release-discovery-options.js";
import type { ReleaseAsset, StableRelease } from "../types/release.js";

const OFFICIAL_RELEASES_ENDPOINT =
  "https://api.github.com/repos/MartinAndreev/conduit/releases?per_page=30";
const DEFAULT_TIMEOUT_MS = 3_000;
const DEFAULT_MAXIMUM_RESPONSE_BYTES = 512 * 1024;
const PRODUCT_USER_AGENT = "conduit-orchestrator-update-check";
const RELEASE_HOSTS = new Set([
  "api.github.com",
  "github.com",
  "objects.githubusercontent.com",
  "release-assets.githubusercontent.com",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function officialHttpsUrl(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "https:" &&
      RELEASE_HOSTS.has(url.hostname) &&
      url.username === "" &&
      url.password === "" &&
      url.port === ""
      ? url.toString()
      : undefined;
  } catch {
    return undefined;
  }
}

function parseAsset(value: unknown): ReleaseAsset | undefined {
  if (!isRecord(value)) return undefined;
  const url = officialHttpsUrl(value.browser_download_url);
  if (
    typeof value.name !== "string" ||
    value.name.length === 0 ||
    value.name.length > 200 ||
    !url ||
    typeof value.size !== "number" ||
    !Number.isSafeInteger(value.size) ||
    value.size < 0
  )
    return undefined;
  return { name: value.name, url, size: value.size };
}

function parseRelease(value: unknown): StableRelease | undefined {
  if (!isRecord(value) || value.draft !== false || value.prerelease !== false)
    return undefined;
  if (
    typeof value.tag_name !== "string" ||
    value.tag_name.length > 100 ||
    !parseSemanticVersion(value.tag_name) ||
    typeof value.published_at !== "string" ||
    !Number.isFinite(Date.parse(value.published_at)) ||
    !Array.isArray(value.assets) ||
    value.assets.length > 100
  )
    return undefined;
  const releaseUrl = officialHttpsUrl(value.html_url);
  if (!releaseUrl) return undefined;
  const assets = value.assets
    .map(parseAsset)
    .filter((asset) => asset !== undefined);
  return {
    version: value.tag_name.startsWith("v")
      ? value.tag_name.slice(1)
      : value.tag_name,
    tagName: value.tag_name,
    publishedAt: value.published_at,
    releaseUrl,
    assets,
  };
}

async function readBoundedBody(
  response: Response,
  maximumBytes: number,
): Promise<string> {
  const contentLength = response.headers.get("content-length");
  if (contentLength && Number(contentLength) > maximumBytes)
    throw new UpdateDiscoveryError(
      "RESPONSE_TOO_LARGE",
      "The release service returned too much data.",
      false,
    );
  if (!response.body)
    throw new UpdateDiscoveryError(
      "EMPTY_RESPONSE",
      "The release service returned an empty response.",
    );

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let received = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      received += value.byteLength;
      if (received > maximumBytes)
        throw new UpdateDiscoveryError(
          "RESPONSE_TOO_LARGE",
          "The release service returned too much data.",
          false,
        );
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const content = new Uint8Array(received);
  let offset = 0;
  for (const chunk of chunks) {
    content.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(content);
}

export class GitHubReleaseDiscovery implements ReleaseDiscovery {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maximumResponseBytes: number;
  private readonly fetchRequest: ReleaseFetch;
  private inFlight?: {
    readonly version: string;
    readonly result: Promise<StableRelease | undefined>;
  };

  constructor(options: ReleaseDiscoveryOptions = {}) {
    this.endpoint = options.endpoint ?? OFFICIAL_RELEASES_ENDPOINT;
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.maximumResponseBytes =
      options.maximumResponseBytes ?? DEFAULT_MAXIMUM_RESPONSE_BYTES;
    this.fetchRequest = options.fetch ?? globalThis.fetch;
    const endpoint = new URL(this.endpoint);
    if (
      endpoint.protocol !== "https:" ||
      endpoint.hostname !== "api.github.com" ||
      endpoint.username !== "" ||
      endpoint.password !== "" ||
      endpoint.port !== "" ||
      endpoint.pathname !== "/repos/MartinAndreev/conduit/releases"
    )
      throw new UpdateDiscoveryError(
        "UNAPPROVED_ENDPOINT",
        "The release endpoint is not approved.",
        false,
      );
  }

  discover(currentVersion: string): Promise<StableRelease | undefined> {
    if (!parseSemanticVersion(currentVersion))
      return Promise.reject(
        new UpdateDiscoveryError(
          "INVALID_CURRENT_VERSION",
          "The running Conduit version is invalid.",
          false,
        ),
      );
    if (this.inFlight?.version === currentVersion) return this.inFlight.result;
    const result = this.request(currentVersion);
    this.inFlight = { version: currentVersion, result };
    return result;
  }

  private async request(
    currentVersion: string,
  ): Promise<StableRelease | undefined> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const response = await this.fetchRequest(this.endpoint, {
        method: "GET",
        headers: {
          accept: "application/vnd.github+json",
          "user-agent": PRODUCT_USER_AGENT,
          "x-github-api-version": "2022-11-28",
        },
        redirect: "manual",
        signal: controller.signal,
      });
      if (response.status >= 300 && response.status < 400)
        throw new UpdateDiscoveryError(
          "REDIRECT_REJECTED",
          "The release service redirected to an unapproved location.",
          false,
        );
      if (response.status === 403 || response.status === 429)
        throw new UpdateDiscoveryError(
          "RATE_LIMITED",
          "The release service is temporarily rate limited.",
        );
      if (!response.ok)
        throw new UpdateDiscoveryError(
          "HTTP_ERROR",
          "The release service is temporarily unavailable.",
        );
      const body = await readBoundedBody(response, this.maximumResponseBytes);
      let parsed: unknown;
      try {
        parsed = JSON.parse(body);
      } catch (cause) {
        throw new UpdateDiscoveryError(
          "INVALID_RESPONSE",
          "The release service returned invalid metadata.",
          false,
          cause,
        );
      }
      if (!Array.isArray(parsed) || parsed.length > 100)
        throw new UpdateDiscoveryError(
          "INVALID_RESPONSE",
          "The release service returned invalid metadata.",
          false,
        );

      const current = parseSemanticVersion(currentVersion);
      if (!current) return undefined;
      return parsed
        .map(parseRelease)
        .filter((release) => release !== undefined)
        .filter((release) => {
          const version = parseSemanticVersion(release.version);
          return version !== undefined && version.prerelease.length === 0;
        })
        .sort((left, right) => {
          const leftVersion = parseSemanticVersion(left.version);
          const rightVersion = parseSemanticVersion(right.version);
          if (!leftVersion || !rightVersion) return 0;
          return compareSemanticVersions(rightVersion, leftVersion);
        })
        .find((release) => {
          const version = parseSemanticVersion(release.version);
          return (
            version !== undefined &&
            compareSemanticVersions(version, current) > 0
          );
        });
    } catch (cause) {
      if (cause instanceof UpdateDiscoveryError) throw cause;
      if (controller.signal.aborted)
        throw new UpdateDiscoveryError(
          "TIMEOUT",
          "The release check timed out.",
          true,
          cause,
        );
      throw new UpdateDiscoveryError(
        "OFFLINE",
        "The release service could not be reached.",
        true,
        cause,
      );
    } finally {
      clearTimeout(timeout);
    }
  }
}
