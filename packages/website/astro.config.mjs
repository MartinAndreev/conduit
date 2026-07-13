import { defineConfig } from "astro/config";

const configuredBase = process.env.BASE_PATH ?? "/";
const base = configuredBase.endsWith("/")
  ? configuredBase
  : `${configuredBase}/`;
const site = process.env.SITE_URL;

export default defineConfig({
  base,
  ...(site ? { site } : {}),
});
