// Minimal static file server with SPA fallback, used by the Playwright visual
// tests. `vite preview` needs native (esbuild/rolldown) binaries that are only
// installed for the build image, so the Playwright image serves `dist/` with
// this dependency-free server instead.

import { createReadStream, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const distDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../dist");
const port = Number.parseInt(process.env.PORT ?? "4173", 10);

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".ico": "image/x-icon",
  ".txt": "text/plain; charset=utf-8",
  ".json": "application/json",
  ".map": "application/json",
};

const indexHtml = path.join(distDir, "index.html");

createServer((request, response) => {
  const url = new URL(request.url ?? "/", `http://localhost:${port}`);
  let file = path.normalize(path.join(distDir, url.pathname));
  if (!file.startsWith(distDir)) {
    response.writeHead(403).end();
    return;
  }
  if (!existsSync(file) || statSync(file).isDirectory()) {
    file = indexHtml;
  }
  const type = MIME[path.extname(file)] ?? "application/octet-stream";
  const immutable = file.startsWith(path.join(distDir, "assets"));
  response.writeHead(200, {
    "content-type": type,
    ...(immutable ? { "cache-control": "public, max-age=31536000, immutable" } : {}),
  });
  createReadStream(file).pipe(response);
}).listen(port, () => {
  console.log(`serving ${distDir} on http://localhost:${port}`);
});
