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
  currentRelease: "releases/v0-7-0/index.html",
  previousRelease: "releases/v0-6-0/index.html",
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

test("release archive and entries render published content", async () => {
  const archive = await readPage(pages.releases);
  assert.match(
    archive,
    /Conduit v0\.7\.0: resilient workspaces and dependable handoffs/u,
  );
  assert.match(archive, /href="\/releases\/v0-7-0\/"/u);
  assert.match(
    archive,
    /Conduit v0\.6\.0: safer updates and clearer workflows/u,
  );
  assert.match(archive, /href="\/releases\/v0-6-0\/"/u);
  assert.match(archive, /Read release notes/u);

  const currentRelease = await readPage(pages.currentRelease);
  assert.match(currentRelease, /durable role workspaces/u);
  assert.match(currentRelease, /Structured ACP response submission/u);
  assert.match(currentRelease, /property="og:type" content="article"/u);
  assert.doesNotMatch(currentRelease, /draft:\s*true/iu);

  const previousRelease = await readPage(pages.previousRelease);
  assert.match(previousRelease, /Guided self-update/u);
});

test("roadmap preserves approved status, priorities, and data-derived counts", async () => {
  const html = await readPage(pages.roadmap);
  const phaseSections = Array.from(
    html.matchAll(
      /<section class="roadmap-column" data-roadmap-column="([^"]+)"[^>]*>(.*?)<\/section>/gsu,
    ),
  );
  assert.deepEqual(
    phaseSections.map((match) => match[1]),
    ["shipped", "development", "research"],
  );

  for (const [, , phaseMarkup] of phaseSections) {
    const renderedCount = Number(
      phaseMarkup.match(/data-roadmap-count[^>]*>(\d+)</u)?.[1],
    );
    const itemCount = phaseMarkup.match(/data-roadmap-item/gu)?.length ?? 0;
    assert.equal(renderedCount, itemCount);
  }

  const entries = Array.from(
    html.matchAll(
      /<li data-roadmap-item data-phase="([^"]+)"(?: data-priority="([^"]+)")?[^>]*>.*?<h4>(.*?)<\/h4>/gsu,
    ),
    (match) => ({ phase: match[1], priority: match[2], title: match[3] }),
  );
  assert.equal(
    entries.length,
    phaseSections.reduce(
      (count, [, , markup]) =>
        count + (markup.match(/data-roadmap-item/gu)?.length ?? 0),
      0,
    ),
  );

  const entryByTitle = new Map(entries.map((entry) => [entry.title, entry]));
  for (const title of [
    "Self update",
    "Local persistence",
    "Universal agent communication",
    "Durable role workspaces and recovery",
    "Approval-gated corrections",
  ]) {
    assert.deepEqual(entryByTitle.get(title), {
      phase: "shipped",
      priority: undefined,
      title,
    });
  }

  assert.deepEqual(entryByTitle.get("Agent long-term memory"), {
    phase: "development",
    priority: "high",
    title: "Agent long-term memory",
  });
  for (const [title, priority] of [
    ["Linear integration", "high"],
    ["Jira integration", "high"],
    ["Asana integration", "normal"],
    ["ClickUp integration", "normal"],
  ]) {
    assert.deepEqual(entryByTitle.get(title), {
      phase: "research",
      priority,
      title,
    });
  }

  assert.ok(
    html.indexOf("Linear integration") < html.indexOf("Asana integration"),
  );
  assert.ok(
    html.indexOf("Jira integration") < html.indexOf("ClickUp integration"),
  );
  assert.match(html, /Turso as the canonical source of truth/u);
  assert.match(html, /Memvid as the default recoverable local index/u);
  assert.match(html, /configurable Hindsight adapter/u);
  assert.match(html, /provider-independent/u);
  assert.match(html, /Planned order and scope may change/u);
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
