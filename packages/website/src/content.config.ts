import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { z } from "astro/zod";

const stableSemVer = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;

const releases = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/releases" }),
  schema: z
    .object({
      title: z.string().min(1),
      description: z.string().min(1),
      publishedAt: z.coerce.date(),
      type: z.enum(["release", "announcement"]),
      version: z.string().regex(stableSemVer).optional(),
      featured: z.boolean().default(false),
      githubReleaseUrl: z
        .url()
        .startsWith("https://github.com/MartinAndreev/conduit/releases/")
        .optional(),
      cover: z
        .object({
          src: z.string().min(1),
          alt: z.string().min(1),
        })
        .optional(),
      draft: z.boolean().default(false),
    })
    .refine(
      (entry) => entry.type === "release" || entry.version !== undefined,
      {
        message: "Announcement entries must include a stable version.",
        path: ["version"],
      },
    ),
});

export const collections = { releases };
