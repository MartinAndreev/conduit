import {
  UpdateDiscoveryError,
  UpdateValidationError,
} from "../errors/update-errors.js";
import type { ReleaseFetch } from "../types/release-discovery-options.js";

const ALLOWED_ASSET_HOSTS = new Set([
  "github.com",
  "objects.githubusercontent.com",
  "release-assets.githubusercontent.com",
]);
const MAXIMUM_REDIRECTS = 3;
const DOWNLOAD_TIMEOUT_MS = 30_000;

function approvedUrl(value: string): URL {
  let url: URL;
  try {
    url = new URL(value);
  } catch (cause) {
    throw new UpdateValidationError(
      "INVALID_ASSET_URL",
      "A release asset URL is invalid.",
      cause,
    );
  }
  if (
    url.protocol !== "https:" ||
    !ALLOWED_ASSET_HOSTS.has(url.hostname) ||
    url.username ||
    url.password ||
    url.port
  )
    throw new UpdateValidationError(
      "UNAPPROVED_ASSET_URL",
      "A release asset URL is not approved.",
    );
  return url;
}

async function boundedBytes(
  response: Response,
  maximumBytes: number,
): Promise<Uint8Array> {
  const contentLength = response.headers.get("content-length");
  if (contentLength && Number(contentLength) > maximumBytes)
    throw new UpdateDiscoveryError(
      "ASSET_TOO_LARGE",
      "The release asset is larger than expected.",
      false,
    );
  if (!response.body)
    throw new UpdateDiscoveryError(
      "EMPTY_ASSET",
      "The release asset response is empty.",
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
          "ASSET_TOO_LARGE",
          "The release asset is larger than expected.",
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
  return content;
}

export class ReleaseAssetFetcher {
  constructor(private readonly fetchRequest: ReleaseFetch = globalThis.fetch) {}

  async fetch(urlValue: string, maximumBytes: number): Promise<Uint8Array> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), DOWNLOAD_TIMEOUT_MS);
    let url = approvedUrl(urlValue);
    try {
      for (let redirects = 0; redirects <= MAXIMUM_REDIRECTS; redirects += 1) {
        const response = await this.fetchRequest(url, {
          method: "GET",
          headers: {
            accept: "application/octet-stream",
            "user-agent": "conduit-orchestrator-update-download",
          },
          redirect: "manual",
          signal: controller.signal,
        });
        if (response.status >= 300 && response.status < 400) {
          if (redirects === MAXIMUM_REDIRECTS)
            throw new UpdateDiscoveryError(
              "TOO_MANY_REDIRECTS",
              "The release asset redirected too many times.",
              false,
            );
          const location = response.headers.get("location");
          if (!location)
            throw new UpdateDiscoveryError(
              "INVALID_REDIRECT",
              "The release asset returned an invalid redirect.",
              false,
            );
          url = approvedUrl(new URL(location, url).toString());
          continue;
        }
        if (!response.ok)
          throw new UpdateDiscoveryError(
            "ASSET_HTTP_ERROR",
            "The release asset is temporarily unavailable.",
          );
        return await boundedBytes(response, maximumBytes);
      }
      throw new UpdateDiscoveryError(
        "TOO_MANY_REDIRECTS",
        "The release asset redirected too many times.",
        false,
      );
    } catch (cause) {
      if (
        cause instanceof UpdateDiscoveryError ||
        cause instanceof UpdateValidationError
      )
        throw cause;
      if (controller.signal.aborted)
        throw new UpdateDiscoveryError(
          "ASSET_TIMEOUT",
          "The release asset download timed out.",
          true,
          cause,
        );
      throw new UpdateDiscoveryError(
        "ASSET_OFFLINE",
        "The release asset could not be downloaded.",
        true,
        cause,
      );
    } finally {
      clearTimeout(timeout);
    }
  }
}
