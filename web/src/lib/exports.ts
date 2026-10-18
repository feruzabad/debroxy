// Signed Link exports and file-name sanitization (docs/frontend/revision2.md
// §5.6.6 and §11 item 4).

export interface Aria2Entry {
  url: string;
  out: string;
}

/** aria2 input file: one entry per link as `{url}\n  out={out}`. */
export function formatAria2(entries: Aria2Entry[]): string {
  return entries.map((entry) => `${entry.url}\n  out=${entry.out}`).join("\n");
}

/** Plain URL list (also read by JDownloader): one URL per line. */
export function formatUrlList(urls: string[]): string {
  return urls.join("\n");
}

/** Removes control characters (U+0000–U+001F and U+007F). */
function stripControlCharacters(value: string): string {
  let out = "";
  for (const char of value) {
    const code = char.charCodeAt(0);
    if (code >= 0x20 && code !== 0x7f) {
      out += char;
    }
  }
  return out;
}

/**
 * A file name for a `download` attribute: the base name with control
 * characters, `/` and `\` removed (§11 item 4).
 */
export function sanitizeBaseName(name: string): string {
  const base = name.split(/[\\/]/).pop() ?? name;
  return stripControlCharacters(base).replace(/[\\/]/g, "");
}

/**
 * A path for an aria2 `out=` line: control characters removed and `..` (and
 * empty) path segments dropped (§11 item 4).
 */
export function sanitizeOutPath(path: string): string {
  return stripControlCharacters(path)
    .split(/[/\\]/)
    .filter((segment) => segment !== ".." && segment !== "." && segment !== "")
    .join("/");
}
