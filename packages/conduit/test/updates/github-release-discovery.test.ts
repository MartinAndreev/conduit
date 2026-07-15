import { test } from "bun:test";
import assert from "node:assert/strict";
import { GitHubReleaseDiscovery } from "../../src/domains/updates/repositories/github-release-discovery.js";
import { UpdateDiscoveryError } from "../../src/domains/updates/errors/update-errors.js";

const release = (
  tag: string,
  overrides: Readonly<Record<string, unknown>> = {},
): Record<string, unknown> => ({
  tag_name: tag,
  draft: false,
  prerelease: false,
  published_at: "2026-07-15T08:00:00Z",
  html_url: `https://github.com/MartinAndreev/conduit/releases/tag/${tag}`,
  assets: [
    {
      name: "conduit-linux-x64",
      browser_download_url:
        "https://github.com/MartinAndreev/conduit/releases/download/v1.0.0/conduit-linux-x64",
      size: 100,
    },
  ],
  ...overrides,
});

function jsonResponse(value: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "content-type": "application/json" },
    ...init,
  });
}

test("discovery selects the newest newer stable SemVer release", async () => {
  const discovery = new GitHubReleaseDiscovery({
    fetch: async () =>
      jsonResponse([
        release("v1.9.0"),
        release("v1.10.0"),
        release("v2.0.0-rc.1", { prerelease: true }),
        release("not-semver"),
        release("v9.0.0", { draft: true }),
      ]),
  });
  const result = await discovery.discover("1.8.0");
  assert.equal(result?.version, "1.10.0");
  assert.equal(result?.assets[0]?.name, "conduit-linux-x64");
});

test("equal and older releases produce no available update", async () => {
  const discovery = new GitHubReleaseDiscovery({
    fetch: async () => jsonResponse([release("v1.2.3"), release("v1.2.2")]),
  });
  assert.equal(await discovery.discover("1.2.3"), undefined);
});

test("concurrent and later consumers share the same process-local request", async () => {
  let requestCount = 0;
  let resolveResponse: ((response: Response) => void) | undefined;
  const responsePromise = new Promise<Response>((resolve) => {
    resolveResponse = resolve;
  });
  const discovery = new GitHubReleaseDiscovery({
    fetch: async () => {
      requestCount += 1;
      return responsePromise;
    },
  });
  const first = discovery.discover("1.0.0");
  const second = discovery.discover("1.0.0");
  resolveResponse?.(jsonResponse([release("v1.1.0")]));
  assert.equal((await first)?.version, "1.1.0");
  assert.equal((await second)?.version, "1.1.0");
  assert.equal((await discovery.discover("1.0.0"))?.version, "1.1.0");
  assert.equal(requestCount, 1);
});

test("discovery sends only bounded product metadata and rejects redirects", async () => {
  let observedUrl = "";
  let observedInit: RequestInit | undefined;
  const discovery = new GitHubReleaseDiscovery({
    fetch: async (input, init) => {
      observedUrl = String(input);
      observedInit = init;
      return new Response(null, {
        status: 302,
        headers: { location: "https://example.com/releases" },
      });
    },
  });
  await assert.rejects(
    discovery.discover("1.0.0"),
    (error: unknown) =>
      error instanceof UpdateDiscoveryError &&
      error.code === "REDIRECT_REJECTED",
  );
  assert.equal(
    observedUrl,
    "https://api.github.com/repos/MartinAndreev/conduit/releases?per_page=30",
  );
  assert.equal(observedInit?.method, "GET");
  assert.equal(observedInit?.redirect, "manual");
  const headers = new Headers(observedInit?.headers);
  assert.equal(headers.get("authorization"), null);
  assert.equal(headers.get("cookie"), null);
  assert.equal(headers.get("user-agent"), "conduit-orchestrator-update-check");
});

test("oversized and invalid JSON responses fail with sanitized domain errors", async () => {
  const oversized = new GitHubReleaseDiscovery({
    maximumResponseBytes: 8,
    fetch: async () => new Response("123456789"),
  });
  await assert.rejects(
    oversized.discover("1.0.0"),
    (error: unknown) =>
      error instanceof UpdateDiscoveryError &&
      error.code === "RESPONSE_TOO_LARGE" &&
      !error.message.includes("123456789"),
  );

  const malformed = new GitHubReleaseDiscovery({
    fetch: async () => new Response("not JSON"),
  });
  await assert.rejects(
    malformed.discover("1.0.0"),
    (error: unknown) =>
      error instanceof UpdateDiscoveryError &&
      error.code === "INVALID_RESPONSE",
  );
});

test("timeout aborts a hanging release request", async () => {
  const discovery = new GitHubReleaseDiscovery({
    timeoutMs: 5,
    fetch: async (_input, init) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () =>
          reject(new DOMException("aborted", "AbortError")),
        );
      }),
  });
  await assert.rejects(
    discovery.discover("1.0.0"),
    (error: unknown) =>
      error instanceof UpdateDiscoveryError && error.code === "TIMEOUT",
  );
});

test("constructor rejects configurable endpoints outside the official repository", () => {
  assert.throws(
    () =>
      new GitHubReleaseDiscovery({
        endpoint: "https://example.com/releases",
      }),
    (error: unknown) =>
      error instanceof UpdateDiscoveryError &&
      error.code === "UNAPPROVED_ENDPOINT",
  );
});
