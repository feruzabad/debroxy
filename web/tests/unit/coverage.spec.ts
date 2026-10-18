import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// API coverage (docs/frontend/revision2.md §15): every server operation the
// client uses must be referenced by its path somewhere in src/, so a new
// server operation fails this test until the client calls it or marks it
// unused.

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../src");
const generatedDir = path.join(srcDir, "api", "generated");

// §15.1: operations the server defines but the client never calls.
const NOT_USED: Record<string, string> = {
  finishOIDCLogin: "the identity provider redirects there; the server redirects to /",
  handleProviderWebhook: "provider-to-server only",
};

// §15.1: operations reached through the SignedLink `url`, not a literal path.
const USED_VIA_SIGNED_LINK_URL = new Set(["download", "downloadHead"]);

function operationPaths(source: string): Map<string, string> {
  const map = new Map<string, string>();
  let currentPath = "";
  for (const line of source.split("\n")) {
    const pathMatch = /^\s*"(\/[^"]+)": \{$/.exec(line);
    if (pathMatch !== null) {
      const operationPath = pathMatch[1];
      if (operationPath !== undefined) {
        currentPath = operationPath;
      }
      continue;
    }
    const opMatch = /^\s*(get|post|put|delete|patch|head): operations\["([^"]+)"\];?$/.exec(line);
    if (opMatch !== null && currentPath !== "") {
      const operationId = opMatch[2];
      if (operationId !== undefined) {
        map.set(operationId, currentPath);
      }
    }
  }
  return map;
}

function listFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      files.push(...listFiles(full));
    } else if (/\.(ts|tsx)$/.test(entry)) {
      files.push(full);
    }
  }
  return files;
}

describe("API coverage (§15)", () => {
  it("references every used operation's path somewhere in src/", () => {
    const operations = new Map<string, string>();
    for (const name of ["identity", "job", "delivery", "client"]) {
      const source = readFileSync(path.join(generatedDir, `${name}.ts`), "utf8");
      for (const [operationId, operationPath] of operationPaths(source)) {
        operations.set(operationId, operationPath);
      }
    }

    const srcFiles = listFiles(srcDir).filter((file) => !file.startsWith(generatedDir));
    const sources = srcFiles.map((file) => readFileSync(file, "utf8")).join("\n");

    expect(operations.size).toBeGreaterThan(0);
    for (const [operationId, operationPath] of operations) {
      if (operationId in NOT_USED) {
        continue;
      }
      if (USED_VIA_SIGNED_LINK_URL.has(operationId)) {
        expect(sources, `${operationId} must consume the SignedLink url`).toContain("link.url");
        continue;
      }
      expect(sources, `${operationId} (${operationPath}) is not referenced in src/`).toContain(
        operationPath,
      );
    }
  });
});
