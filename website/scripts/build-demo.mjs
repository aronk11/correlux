import { cp, mkdir, mkdtemp, readFile, writeFile, rm, chmod, readdir } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const output = path.join(root, "website/dist/assets/demo");
const work = await mkdtemp(path.join(tmpdir(), "correlux-browser-"));
const go = (args, env = {}) => execFileSync("go", args, { cwd: root, env: { ...process.env, ...env }, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] }).trim();
async function writableDirectories(dir) {
  await chmod(dir, 0o755);
  for (const item of await readdir(dir, { withFileTypes: true })) {
    if (item.isDirectory()) await writableDirectories(path.join(dir, item.name));
  }
}
try {
  await mkdir(output, { recursive: true });
  // `go mod download` rather than `go list -m`: the source has to be on disk
  // to be patched, and `go list` reports no directory for a module that has
  // not been extracted yet. On a developer's machine the cache is warm and
  // both answer; on a clean CI runner only this one does.
  const upstream = JSON.parse(go(["mod", "download", "-json", "charm.land/bubbletea/v2"]));
  if (!upstream.Dir) throw new Error(`no source directory for ${upstream.Path}@${upstream.Version}`);
  const tea = path.join(work, "bubbletea");
  await cp(upstream.Dir, tea, { recursive: true });
  await writableDirectories(tea);
  await cp(path.join(root, "website/scripts/wasm/tea_js.go.txt"), path.join(tea, "correlux_browser_js.go"));
  const mod = path.join(work, "demo.mod");
  await writeFile(mod, await readFile(path.join(root, "go.mod"), "utf8") + `\nreplace charm.land/bubbletea/v2 => ${JSON.stringify(tea)}\n`);
  await cp(path.join(root, "go.sum"), path.join(work, "demo.sum"));
  go(["build", "-buildvcs=false", `-modfile=${mod}`, "-tags=correlux_demo", "-ldflags=-s -w", "-o", path.join(output, "correlux.wasm"), "./cmd/correlux-demo"], { GOOS: "js", GOARCH: "wasm" });
  const wasm = path.join(output, "correlux.wasm");
  await writeFile(wasm + ".gz", gzipSync(await readFile(wasm), { level: 9 }));
  await rm(wasm);
  const goroot = go(["env", "GOROOT"]);
  await cp(path.join(goroot, "lib/wasm/wasm_exec.js"), path.join(output, "wasm_exec.js"));
  try {
    await cp(path.join(goroot, "LICENSE"), path.join(output, "GO-LICENSE.txt"));
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    await cp(path.join(goroot, "../LICENSE"), path.join(output, "GO-LICENSE.txt"));
  }
  console.log("Built the real Correlux terminal UI for the browser (fixture-only).");
} finally {
  await rm(work, { recursive: true, force: true });
}
