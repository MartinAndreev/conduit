export interface ReleaseAsset {
  readonly name: string;
  readonly url: string;
  readonly size: number;
}

export interface StableRelease {
  readonly version: string;
  readonly tagName: string;
  readonly publishedAt: string;
  readonly releaseUrl: string;
  readonly assets: readonly ReleaseAsset[];
}
