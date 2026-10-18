// Size budget check (docs/frontend/revision3.md §12). Run after `vite build`.
// It reads the Vite manifest, computes the initial JS for `/login` and for `/`
// (the entry chunk plus every statically imported chunk, and the route's own
// code-split chunks), gzip-sizes them, and checks the CSS. Exits 1 when a
// budget is exceeded.

import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const distDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../dist");
const manifestPath = path.join(distDir, ".vite", "manifest.json");

const KiB = 1024;

const BUDGETS = {
  loginJs: 200 * KiB,
  rootJs: 250 * KiB,
  css: 30 * KiB,
};

// §4.1: the shell (`_user`) and the `/` route are part of the user landing
// chunk; `/login` is its own page. Route chunks are matched by the route file,
// because the manifest keys add a `?tsr-split=…` suffix.
const PAGE_ROUTES = {
  login: ["src/routes/login.tsx"],
  root: ["src/routes/_user.tsx", "src/routes/_user/index.tsx"],
};

if (!existsSync(manifestPath)) {
  console.error(`error: ${manifestPath} not found; run \`vite build\` first`);
  process.exit(1);
}

const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));

function gzipSize(file) {
  const full = path.join(distDir, file);
  return existsSync(full) ? gzipSync(readFileSync(full)).length : 0;
}

/** The set of chunk keys reachable from `keys` through static `imports`. */
function staticClosure(keys) {
  const seen = new Set();
  const stack = [...keys];
  while (stack.length > 0) {
    const key = stack.pop();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    for (const imported of manifest[key]?.imports ?? []) {
      if (!seen.has(imported)) {
        stack.push(imported);
      }
    }
  }
  return seen;
}

/** The gzip size of every unique `.js` chunk in `keys`. */
function jsSize(keys) {
  const files = new Set();
  for (const key of keys) {
    const entry = manifest[key];
    if (entry?.file?.endsWith(".js")) {
      files.add(entry.file);
    }
  }
  let total = 0;
  for (const file of files) {
    total += gzipSize(file);
  }
  return total;
}

/** The dynamic route chunks for a page, keyed by their route file paths. */
function routeChunks(routeFiles) {
  const keys = [];
  for (const [key, entry] of Object.entries(manifest)) {
    if (entry.isDynamicEntry && routeFiles.some((file) => key.startsWith(file))) {
      keys.push(key);
    }
  }
  return keys;
}

function measurePage(routeFiles) {
  const keys = staticClosure([...routeChunks(routeFiles), ...entryChunks()]);
  return jsSize(keys);
}

function entryChunks() {
  return Object.keys(manifest).filter((key) => manifest[key].isEntry);
}

function cssSize() {
  const files = new Set();
  for (const entry of Object.values(manifest)) {
    for (const file of entry.css ?? []) {
      files.add(file);
    }
  }
  let total = 0;
  for (const file of files) {
    total += gzipSize(file);
  }
  return total;
}

const measured = {
  loginJs: measurePage(PAGE_ROUTES.login),
  rootJs: measurePage(PAGE_ROUTES.root),
  css: cssSize(),
};

const label = (name) => name.padEnd(22);
let failed = false;

console.log("Initial JS (gzip):");
for (const [name, budget] of Object.entries(BUDGETS)) {
  const value = measured[name];
  const ok = value <= budget;
  failed = failed || !ok;
  const valueKiB = (value / KiB).toFixed(1);
  const budgetKiB = (budget / KiB).toFixed(1);
  const marker = ok ? "ok " : "FAIL";
  console.log(`  ${label(name)} ${valueKiB.padStart(7)} KiB  (budget ${budgetKiB} KiB)  ${marker}`);
}

if (failed) {
  console.error("\nsize budget exceeded; see docs/frontend/revision3.md §12");
  process.exit(1);
}

console.log("\nall size budgets pass");
