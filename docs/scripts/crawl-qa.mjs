#!/usr/bin/env node
// Crawl-QA checks for the built docs site (docs/dist).
//
// Runs entirely offline against the static build output — no network calls,
// no extra dependencies (keeps the docs lockfile untouched). Checks:
//
//   1. Broken internal links   — every site-root-relative href/src resolves
//      to a file that actually exists in the build output.
//   2. Duplicate <title>       — two pages must not share an exact title.
//   3. Duplicate/missing <h1>  — every page has exactly one <h1>; H1 text
//      must not be duplicated across pages.
//   4. Missing meta description — every page has a non-empty
//      <meta name="description" content="...">.
//   5. Sitemap URLs            — every <loc> in the sitemap resolves to a
//      file in the build output (a same-content-set proxy for "would 200").
//
// Usage: node crawl-qa.mjs <path-to-dist>  (defaults to ./dist)
//
// Exits non-zero if any check finds a real problem.

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { join, relative, sep } from "node:path";

const distArg = process.argv[2] ?? "dist";
const distDir = distArg.startsWith("/") ? distArg : join(process.cwd(), distArg);

if (!existsSync(distDir)) {
  console.error(`crawl-qa: dist directory not found: ${distDir}`);
  console.error(`Build the docs first (e.g. 'bun run build') before running this check.`);
  process.exit(1);
}

/** Recursively list all files under `dir`. */
function walk(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const st = statSync(full);
    if (st.isDirectory()) {
      walk(full, out);
    } else {
      out.push(full);
    }
  }
  return out;
}

const allFiles = walk(distDir);
const htmlFiles = allFiles
  .filter((f) => f.endsWith(".html"))
  // Pagefind ships its own generated search-result fragments; not real pages.
  .filter((f) => !relative(distDir, f).split(sep).includes("pagefind"));

/** Convert a dist-relative file path to its site URL path (posix, leading slash). */
function toUrlPath(file) {
  let rel = relative(distDir, file).split(sep).join("/");
  if (rel === "index.html") return "/";
  if (rel.endsWith("/index.html")) return "/" + rel.slice(0, -"index.html".length);
  return "/" + rel;
}

// Build the set of paths that actually exist in the build output, keyed by
// their URL form with a trailing slash stripped (except root) so lookups are
// tolerant of the trailing-slash-or-not question — that's a separate URL
// contract concern, not a "does this resource exist" concern.
function normalize(p) {
  if (p === "/") return "/";
  return p.replace(/\/+$/, "");
}

const existingUrlPaths = new Set();
for (const f of allFiles) {
  const rel = "/" + relative(distDir, f).split(sep).join("/");
  existingUrlPaths.add(normalize(rel));
  if (rel.endsWith("/index.html")) {
    existingUrlPaths.add(normalize(rel.slice(0, -"index.html".length)));
  }
}

const pages = htmlFiles.map((file) => ({
  file,
  url: toUrlPath(file),
  html: readFileSync(file, "utf8"),
}));

const problems = { brokenLinks: [], dupTitles: [], h1Issues: [], dupH1s: [], missingDescription: [], badSitemapUrls: [] };

function stripTags(s) {
  return s.replace(/<[^>]+>/g, "").replace(/\s+/g, " ").trim();
}

// --- 1. Broken internal links -------------------------------------------

const linkAttrRe = /<(?:a|link)\s+[^>]*?href="([^"]+)"[^>]*>|<img\s+[^>]*?src="([^"]+)"[^>]*>/gi;

for (const page of pages) {
  let m;
  linkAttrRe.lastIndex = 0;
  while ((m = linkAttrRe.exec(page.html))) {
    const raw = m[1] ?? m[2];
    if (!raw) continue;
    if (!raw.startsWith("/")) continue; // external, mailto:, tel:, protocol-relative, anchors, etc.
    if (raw.startsWith("//")) continue; // protocol-relative external
    const withoutHash = raw.split("#")[0];
    if (withoutHash === "") continue; // pure in-page anchor like "/#foo" -> after split becomes "/" which is fine, but a lone "#foo" is caught by startsWith("/") already
    const withoutQuery = withoutHash.split("?")[0];
    const target = normalize(withoutQuery);
    if (!existingUrlPaths.has(target)) {
      problems.brokenLinks.push({ page: page.url, link: raw });
    }
  }
}

// --- 2/3. Titles and H1s --------------------------------------------------

const titleMap = new Map(); // title -> [urls]
const h1Map = new Map(); // h1 text -> [urls]

for (const page of pages) {
  const titleMatch = page.html.match(/<title>([\s\S]*?)<\/title>/i);
  const title = titleMatch ? stripTags(titleMatch[1]) : "";
  if (title) {
    if (!titleMap.has(title)) titleMap.set(title, []);
    titleMap.get(title).push(page.url);
  }

  const h1Matches = [...page.html.matchAll(/<h1[^>]*>([\s\S]*?)<\/h1>/gi)];
  if (h1Matches.length === 0) {
    problems.h1Issues.push({ page: page.url, issue: "no <h1> found" });
  } else if (h1Matches.length > 1) {
    problems.h1Issues.push({ page: page.url, issue: `${h1Matches.length} <h1> elements found` });
  }
  if (h1Matches.length > 0) {
    const h1 = stripTags(h1Matches[0][1]);
    if (h1) {
      if (!h1Map.has(h1)) h1Map.set(h1, []);
      h1Map.get(h1).push(page.url);
    }
  }
}

for (const [title, urls] of titleMap) {
  if (urls.length > 1) problems.dupTitles.push({ title, urls });
}
for (const [h1, urls] of h1Map) {
  if (urls.length > 1) problems.dupH1s.push({ h1, urls });
}

// --- 4. Missing meta description ------------------------------------------

const metaDescRe = /<meta\s+([^>]*?)>/gi;
for (const page of pages) {
  let found = "";
  let m;
  metaDescRe.lastIndex = 0;
  while ((m = metaDescRe.exec(page.html))) {
    const attrs = m[1];
    if (!/name=["']description["']/i.test(attrs)) continue;
    const contentMatch = attrs.match(/content=["']([^"']*)["']/i);
    found = contentMatch ? contentMatch[1].trim() : "";
    break;
  }
  if (!found) {
    problems.missingDescription.push(page.url);
  }
}

// --- 5. Sitemap URLs -------------------------------------------------------

const sitemapIndexPath = join(distDir, "sitemap-index.xml");
if (existsSync(sitemapIndexPath)) {
  const indexXml = readFileSync(sitemapIndexPath, "utf8");
  const childSitemapLocs = [...indexXml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]);

  const allUrls = [];
  for (const loc of childSitemapLocs) {
    let childPath;
    try {
      childPath = new URL(loc).pathname;
    } catch {
      continue;
    }
    const localChild = join(distDir, childPath.replace(/^\//, ""));
    if (!existsSync(localChild)) {
      problems.badSitemapUrls.push({ url: loc, reason: "sitemap file itself missing from build output" });
      continue;
    }
    const childXml = readFileSync(localChild, "utf8");
    for (const m of childXml.matchAll(/<loc>([^<]+)<\/loc>/g)) {
      allUrls.push(m[1]);
    }
  }

  for (const url of allUrls) {
    let pathname;
    try {
      pathname = new URL(url).pathname;
    } catch {
      problems.badSitemapUrls.push({ url, reason: "not a valid absolute URL" });
      continue;
    }
    if (!existingUrlPaths.has(normalize(pathname))) {
      problems.badSitemapUrls.push({ url, reason: "no matching page in build output" });
    }
  }
} else {
  console.warn("crawl-qa: no sitemap-index.xml found in dist — skipping sitemap check.");
}

// --- Report ----------------------------------------------------------------

let hasProblems = false;

function section(name, items, formatItem) {
  if (items.length === 0) return;
  hasProblems = true;
  console.log(`\n${name} (${items.length}):`);
  for (const item of items) console.log(`  - ${formatItem(item)}`);
}

section("Broken internal links", problems.brokenLinks, (i) => `${i.page} -> ${i.link}`);
section("Duplicate <title>", problems.dupTitles, (i) => `"${i.title}" used by: ${i.urls.join(", ")}`);
section("H1 issues", problems.h1Issues, (i) => `${i.page}: ${i.issue}`);
section("Duplicate <h1> text", problems.dupH1s, (i) => `"${i.h1}" used by: ${i.urls.join(", ")}`);
section("Missing meta description", problems.missingDescription, (i) => i);
section("Sitemap URLs not in build output", problems.badSitemapUrls, (i) => `${i.url} (${i.reason})`);

console.log(`\ncrawl-qa: scanned ${pages.length} pages.`);

if (hasProblems) {
  console.log("crawl-qa: FAILED — see problems above.");
  process.exit(1);
}

console.log("crawl-qa: all checks passed.");
