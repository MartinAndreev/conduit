import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const packageRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const distRoot = path.join(packageRoot, "dist");

const pages = {
  home: "index.html",
  docs: "docs/index.html",
  releases: "releases/index.html",
  announcement: "releases/v0-5-1/index.html",
  roadmap: "roadmap/index.html",
};

async function readPage(relativePath) {
  return readFile(path.join(distRoot, relativePath), "utf8");
}

test("build emits every Feature 006 route", async () => {
  await Promise.all(
    Object.values(pages).map((relativePath) =>
      access(path.join(distRoot, relativePath)),
    ),
  );
});

test("shared navigation is ordered and marks the active page", async () => {
  for (const [page, relativePath] of Object.entries(pages)) {
    const html = await readPage(relativePath);
    const primaryNavigation = html.match(
      /<nav class="primary-nav"[^>]*>(.*?)<\/nav>/u,
    )?.[1];
    assert.ok(primaryNavigation, `${page} has primary navigation`);

    const labels = ["Home", "Docs", "Releases", "Roadmap", "GitHub"];
    let priorIndex = -1;
    for (const label of labels) {
      const currentIndex = primaryNavigation.indexOf(`>${label}</a>`);
      assert.ok(currentIndex > priorIndex, `${page} orders ${label} correctly`);
      priorIndex = currentIndex;
    }
  }

  const releases = await readPage(pages.releases);
  assert.match(releases, /href="\/releases\/" aria-current="page"/u);
  const roadmap = await readPage(pages.roadmap);
  assert.match(roadmap, /href="\/roadmap\/" aria-current="page"/u);
});

test("release archive and announcement render published content", async () => {
  const archive = await readPage(pages.releases);
  assert.match(archive, /Major announcement/u);
  assert.match(archive, /Conduit v0\.5\.1: a durable local foundation/u);
  assert.match(archive, /href="\/releases\/v0-5-1\/"/u);

  const announcement = await readPage(pages.announcement);
  assert.match(announcement, /embedded Turso database/u);
  assert.match(announcement, /property="og:type" content="article"/u);
  assert.doesNotMatch(announcement, /draft:\s*true/iu);
});

test("roadmap preserves approved phases, priorities, and ordering", async () => {
  const html = await readPage(pages.roadmap);
  for (const item of [
    "Self update",
    "Agent memory",
    "Linear integration",
    "Jira integration",
    "Asana integration",
    "ClickUp integration",
  ]) {
    assert.match(html, new RegExp(`>${item}<`, "u"));
  }

  assert.ok(
    html.indexOf("Linear integration") < html.indexOf("Asana integration"),
  );
  assert.ok(
    html.indexOf("Jira integration") < html.indexOf("ClickUp integration"),
  );
  assert.match(
    html,
    /data-phase="development" data-priority="high"[^>]*><article[^>]*>.*?Self update/su,
  );
  assert.match(
    html,
    /data-phase="research" data-priority="high"[^>]*><article[^>]*>.*?Linear integration/su,
  );
  assert.match(html, /Roadmap order and scope may change/u);
});

test("drawer ships its accessibility and dismissal controls", async () => {
  const html = await readPage(pages.home);
  assert.match(html, /aria-expanded="false"/u);
  assert.match(html, /aria-controls="site-navigation-drawer"/u);
  assert.match(html, /aria-modal="true" role="dialog"/u);
  assert.match(html, /data-drawer-backdrop/u);
  assert.match(html, /\.key===`Escape`/u);
  assert.match(html, /document\.body\.style\.overflow=`hidden`/u);
  assert.match(html, /\.inert=/u);
});

test("all generated internal links resolve inside static output", async () => {
  for (const relativePath of Object.values(pages)) {
    const html = await readPage(relativePath);
    const routeUrl = new URL(
      relativePath.replace(/index\.html$/u, ""),
      "https://local/",
    );
    const hrefs = Array.from(
      html.matchAll(/href="([^"]+)"/gu),
      (match) => match[1],
    );

    for (const href of hrefs) {
      if (
        href.startsWith("#") ||
        href.startsWith("mailto:") ||
        /^https?:\/\//u.test(href)
      )
        continue;

      const pathname = decodeURIComponent(new URL(href, routeUrl).pathname);
      const relativeTarget = pathname.replace(/^\//u, "");
      const target = path.extname(relativeTarget)
        ? path.join(distRoot, relativeTarget)
        : path.join(distRoot, relativeTarget, "index.html");
      await assert.doesNotReject(
        access(target),
        `${relativePath} links to generated ${pathname}`,
      );
    }
  }
});
