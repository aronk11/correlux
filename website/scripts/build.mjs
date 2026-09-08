import { readFile, writeFile, mkdir, readdir, cp, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Marked } from "marked";

const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const site = path.join(root, "website");
const out = path.join(site, "dist");
const repository = "https://github.com/aronk11/correlux";
const siteURL = (
  process.env.SITE_URL || "https://aronk11.github.io/correlux/"
).replace(/\/?$/, "/");
const escape = (text) =>
  String(text).replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const slug = (text) =>
  text
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/[^\p{L}\p{N}_\s-]/gu, "")
    .trim()
    .replace(/\s/g, "-");
const pages = [
  {
    source: "website/docs-index.md",
    output: "index.html",
    title: "Documentation",
    group: "GET STARTED",
  },
  {
    source: "website/getting-started.md",
    output: "getting-started.html",
    title: "Getting started",
    group: "GET STARTED",
  },
  {
    source: "README.md",
    output: "guide.html",
    title: "User guide",
    group: "USING CORRELUX",
  },
  {
    source: "docs/architecture.md",
    output: "architecture.html",
    title: "Architecture",
    group: "UNDER THE HOOD",
  },
  {
    source: "docs/adr/README.md",
    output: "decisions.html",
    title: "Design decisions",
    group: "UNDER THE HOOD",
  },
  {
    source: "SPEC.md",
    output: "specification.html",
    title: "Product specification",
    group: "UNDER THE HOOD",
  },
  {
    source: "CONTRIBUTING.md",
    output: "contributing.html",
    title: "Contributing",
    group: "COMMUNITY",
  },
  {
    source: "CODE_OF_CONDUCT.md",
    output: "code-of-conduct.html",
    title: "Code of conduct",
    group: "COMMUNITY",
  },
  {
    source: "SECURITY.md",
    output: "security.html",
    title: "Security",
    group: "COMMUNITY",
  },
];
for (const name of (await readdir(path.join(root, "docs/adr"))).sort()) {
  if (name === "README.md" || !name.endsWith(".md")) continue;
  const source = `docs/adr/${name}`;
  const md = await readFile(path.join(root, source), "utf8");
  pages.push({
    source,
    output: name.replace(/\.md$/, ".html"),
    title: md.match(/^# (.+)$/m)?.[1] || name,
    group: "DECISION RECORDS",
  });
}
const destinations = new Map(pages.map((page) => [page.source, page.output]));
await rm(out, { recursive: true, force: true });
await mkdir(path.join(out, "docs"), { recursive: true });
await cp(path.join(site, "assets"), path.join(out, "assets"), {
  recursive: true,
});
await writeFile(
  path.join(out, "index.html"),
  (await readFile(path.join(site, "index.html"), "utf8")).replaceAll(
    "{{SITE_URL}}",
    escape(siteURL),
  ),
);
await writeFile(path.join(out, ".nojekyll"), "");
const searchIndex = [];
for (const page of pages) {
  const headings = [];
  const used = new Map();
  const marked = new Marked({
    gfm: true,
    renderer: {
      heading({ tokens, depth }) {
        const text = this.parser.parseInline(tokens);
        const plain = text.replace(/<[^>]+>/g, "");
        const base = slug(plain);
        const count = used.get(base) || 0;
        used.set(base, count + 1);
        const id = count ? `${base}-${count}` : base;
        headings.push({ depth, text: plain, id });
        return `<h${depth} id="${escape(id)}">${text}</h${depth}>\n`;
      },
      link({ href, title, tokens }) {
        let target = href;
        if (!/^(?:[a-z]+:|\/\/|#)/i.test(href)) {
          const [file, hash] = href.split("#");
          const resolved = path.posix.normalize(
            path.posix.join(path.posix.dirname(page.source), file),
          );
          target = destinations.has(resolved)
            ? `${destinations.get(resolved)}${hash ? `#${hash}` : ""}`
            : `${repository}/blob/main/${resolved}${hash ? `#${hash}` : ""}`;
        }
        if (!/^(?:https?:|mailto:|#|[^:]+$)/i.test(target)) target = "#";
        return `<a href="${escape(target)}"${title ? ` title="${escape(title)}"` : ""}>${this.parser.parseInline(tokens)}</a>`;
      },
      table(token) {
        return `<div class="table-scroll" tabindex="0" role="region" aria-label="Scrollable reference table">${this.constructor.prototype.table.call(this, token)}</div>`;
      },
    },
  });
  const md = await readFile(path.join(root, page.source), "utf8");
  const body = marked.parse(md);
  let previousGroup;
  let links = "";
  for (const nav of pages.filter((p) => p.group !== "DECISION RECORDS")) {
    if (nav.group !== previousGroup)
      links += `<div class="sidebar-label">${nav.group}</div>`;
    previousGroup = nav.group;
    links += `<a href="${nav.output}"${page.output === nav.output ? ' aria-current="page"' : ""}>${escape(nav.title)}</a>`;
  }
  links += `<details${page.group === "DECISION RECORDS" ? " open" : ""}><summary>ALL 20 DESIGN DECISIONS</summary>`;
  for (const nav of pages.filter((p) => p.group === "DECISION RECORDS"))
    links += `<a href="${nav.output}"${page.output === nav.output ? ' aria-current="page"' : ""}>${escape(nav.title)}</a>`;
  links += "</details>";
  const navigation = `<div class="sidebar-links">${links}</div>`;
  const toc = headings
    .filter((h) => h.depth === 2)
    .map((h) => `<a href="#${escape(h.id)}">${h.text}</a>`)
    .join("");
  const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${escape(page.title)} — Correlux docs</title><meta name="description" content="${escape(page.title)} for Correlux, the application-first Kubernetes terminal UI."><meta name="theme-color" content="#11151d"><link rel="canonical" href="${escape(siteURL)}docs/${page.output}"><link rel="icon" href="../assets/logo.svg" type="image/svg+xml"><link rel="stylesheet" href="../assets/style.css"><script src="../assets/main.js" defer></script></head><body>
<a class="skip-link" href="#main">Skip to content</a><header class="site-header"><a class="brand" href="../" aria-label="Correlux home"><img src="../assets/logo.svg" width="34" height="34" alt="">correlux<span class="brand-dot">_</span></a><nav aria-label="Main navigation"><a href="../#features">The good stuff</a><a href="./" aria-current="page">Docs</a><a href="${repository}" class="github-link">GitHub ↗</a></nav><a class="button button-small" href="../#install">Get Correlux ↗</a></header>
<div class="docs-layout"><aside class="docs-sidebar" aria-label="Documentation navigation"><button class="docs-search" data-search>Search docs <kbd>⌘ / Ctrl K</kbd></button>${navigation}<details class="docs-mobile-nav"><summary>Browse documentation</summary>${navigation}</details></aside><main class="doc-article" id="main"><div class="doc-breadcrumb"><a href="./">DOCS</a> / ${escape(page.title.toUpperCase())}</div><article class="prose">${body}</article><a class="doc-edit" href="${repository}/edit/main/${page.source}">Improve this page on GitHub ↗</a></main><nav class="docs-toc" aria-label="On this page"><span>ON THIS PAGE</span>${toc}</nav></div>
<footer class="site-footer section-wrap"><a class="brand" href="../"><img src="../assets/logo.svg" width="28" height="28" alt="">correlux<span class="brand-dot">_</span></a><span>Less cluster. More clarity.</span><nav aria-label="Footer"><a href="contributing.html">Contribute</a><a href="${repository}/issues">Issues</a><a href="${repository}/blob/main/LICENSE">Apache 2.0</a></nav></footer>
<dialog class="search-dialog" aria-labelledby="search-label"><div class="search-top"><label id="search-label" class="sr-only" for="docs-query">Search documentation</label><input id="docs-query" type="search" placeholder="Search documentation…" autocomplete="off"><button class="search-close" aria-label="Close search">ESC</button></div><div class="search-results"></div><div class="search-foot search-count" role="status" aria-live="polite">Search locally. No account. No tracking.</div></dialog><div class="toast" role="status" aria-live="polite"></div></body></html>`;
  await writeFile(path.join(out, "docs", page.output), html);
  const sections = body.split(/(?=<h[12] id=)/);
  for (const section of sections) {
    const heading = section.match(/^<h[12] id="([^"]+)">([\s\S]*?)<\/h[12]>/);
    if (!heading) continue;
    const text = section
      .replace(/<[^>]+>/g, " ")
      .replace(
        /&(?:amp|lt|gt|quot|#39);/g,
        (x) =>
          ({
            "&amp;": "&",
            "&lt;": "<",
            "&gt;": ">",
            "&quot;": '"',
            "&#39;": "'",
          })[x],
      )
      .replace(/\s+/g, " ")
      .trim();
    searchIndex.push({
      title: `${page.title} / ${heading[2].replace(/<[^>]+>/g, "")}`,
      href: `${page.output}#${heading[1]}`,
      text,
    });
  }
}
await writeFile(
  path.join(out, "docs/search.json"),
  JSON.stringify(searchIndex),
);
await writeFile(
  path.join(out, "sitemap.xml"),
  `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>${escape(siteURL)}</loc></url>${pages.map((p) => `<url><loc>${escape(siteURL)}docs/${p.output}</loc></url>`).join("")}</urlset>`,
);
await writeFile(
  path.join(out, "robots.txt"),
  `User-agent: *\nAllow: /\nSitemap: ${siteURL}sitemap.xml\n`,
);
await writeFile(
  path.join(out, "404.html"),
  `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>404 — Correlux</title><style>body{background:#11151d;color:#252823;font:18px system-ui;padding:15vh 8vw}h1{font-size:clamp(45px,8vw,90px);letter-spacing:-.05em}a{color:#ae3b1c}p{color:#64665c}</style><main><p>404 / RESOURCE NOT FOUND</p><h1>This page is unschedulable.</h1><p>Even our docs have the occasional missing endpoint.</p><a href="${escape(siteURL)}">Back to mission control →</a></main></html>`,
);
console.log(
  `Built homepage + ${pages.length} documentation pages, ${searchIndex.length} searchable sections → website/dist`,
);
