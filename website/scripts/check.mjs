import { readFile, readdir, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../dist",
);
async function walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map((entry) =>
        entry.isDirectory()
          ? walk(path.join(dir, entry.name))
          : path.join(dir, entry.name),
      ),
    )
  ).flat();
}
const files = await walk(root);
const pages = files.filter((file) => file.endsWith(".html"));
const ids = new Map();
const errors = [];
for (const file of pages) {
  const html = await readFile(file, "utf8");
  const matches = [...html.matchAll(/\bid="([^"]+)"/g)].map(
    (match) => match[1],
  );
  if (new Set(matches).size !== matches.length)
    errors.push(`${path.relative(root, file)}: duplicate IDs`);
  ids.set(file, new Set(matches));
}
for (const file of pages) {
  const html = await readFile(file, "utf8");
  for (const [, link] of html.matchAll(/\b(?:href|src)="([^"]+)"/g)) {
    if (/^(?:https?:|mailto:|data:)/.test(link)) continue;
    const [pathname, hash] = link.split("#");
    let target = pathname ? path.resolve(path.dirname(file), pathname) : file;
    try {
      if ((await stat(target)).isDirectory())
        target = path.join(target, "index.html");
      await stat(target);
      if (
        hash &&
        ids.has(target) &&
        !ids.get(target).has(decodeURIComponent(hash))
      )
        errors.push(`${path.relative(root, file)}: missing anchor ${link}`);
    } catch {
      errors.push(`${path.relative(root, file)}: missing target ${link}`);
    }
  }
}
const search = JSON.parse(
  await readFile(path.join(root, "docs/search.json"), "utf8"),
);
for (const item of search) {
  const [page, hash] = item.href.split("#");
  if (!ids.get(path.join(root, "docs", page))?.has(hash))
    errors.push(`Invalid search result: ${item.href}`);
}
if (errors.length) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(
  `Checked ${pages.length} HTML pages and ${search.length} search results: all local links, anchors and IDs valid.`,
);
