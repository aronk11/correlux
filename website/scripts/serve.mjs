import http from "node:http";
import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../dist",
);
const port = Number(process.env.PORT || 4173);
const types = {
  ".wasm": "application/wasm",
  ".html": "text/html; charset=utf-8",
  ".css": "text/css",
  ".js": "text/javascript",
  ".svg": "image/svg+xml",
  ".json": "application/json",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".xml": "application/xml",
  ".txt": "text/plain",
};
http
  .createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost");
      let pathname = decodeURIComponent(url.pathname);
      // Also exercise deployment under the GitHub project-pages prefix.
      if (pathname === "/correlux") {
        response.writeHead(301, { Location: "/correlux/" });
        response.end();
        return;
      }
      if (pathname.startsWith("/correlux/"))
        pathname = pathname.slice("/correlux".length);
      let file = path.resolve(root, `.${pathname}`);
      if (file !== root && !file.startsWith(root + path.sep)) {
        response.writeHead(403);
        response.end();
        return;
      }
      if ((await stat(file)).isDirectory()) {
        if (!url.pathname.endsWith("/")) {
          response.writeHead(301, {
            Location: `${url.pathname}/${url.search}`,
          });
          response.end();
          return;
        }
        file = path.join(file, "index.html");
      }
      response.writeHead(200, {
        "Content-Type": types[path.extname(file)] || "application/octet-stream",
      });
      response.end(await readFile(file));
    } catch {
      response.writeHead(404, { "Content-Type": "text/html; charset=utf-8" });
      response.end(await readFile(path.join(root, "404.html")));
    }
  })
  .listen(port, "127.0.0.1", () =>
    console.log(`Correlux preview: http://127.0.0.1:${port}/correlux/`),
  );
