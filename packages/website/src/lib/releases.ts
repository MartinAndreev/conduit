import type { CollectionEntry } from "astro:content";

export type ReleaseEntry = CollectionEntry<"releases">;

export function getPublishedReleases(
  entries: readonly ReleaseEntry[],
): readonly ReleaseEntry[] {
  return entries
    .filter((entry) => !entry.data.draft)
    .toSorted((left, right) => {
      const dateDifference =
        right.data.publishedAt.getTime() - left.data.publishedAt.getTime();
      return dateDifference || left.id.localeCompare(right.id);
    });
}

export function formatReleaseDate(date: Date): string {
  return new Intl.DateTimeFormat("en", {
    day: "numeric",
    month: "long",
    timeZone: "UTC",
    year: "numeric",
  }).format(date);
}
