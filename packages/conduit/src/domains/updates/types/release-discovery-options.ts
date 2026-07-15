export type ReleaseFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>;

export interface ReleaseDiscoveryOptions {
  readonly endpoint?: string;
  readonly timeoutMs?: number;
  readonly maximumResponseBytes?: number;
  readonly fetch?: ReleaseFetch;
}
