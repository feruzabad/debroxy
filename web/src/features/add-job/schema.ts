import { z } from "zod";

export const MAGNET_MAX_BYTES = 16 * 1024;
export const WEB_URL_MAX_BYTES = 8 * 1024;

/** The `/add` route search params (§4.1). */
export const AddJobSearch = z.object({
  source: z.enum(["magnet", "torrent", "web"]).optional().catch(undefined),
  provider: z.string().optional().catch(undefined),
});

export type AddJobSearch = z.output<typeof AddJobSearch>;

const MAGNET_ERROR = "This doesn't look like a magnet link.";

function decodeSafe(value: string): string | null {
  try {
    return decodeURIComponent(value);
  } catch {
    return null;
  }
}

/** §5.5.3: a magnet with a `urn:btih:` (40 hex / 32 base32) or `urn:btmh:1220` (64 hex) hash. */
export function magnetHasValidHash(magnet: string): boolean {
  if (!magnet.startsWith("magnet:?")) {
    return false;
  }
  const query = magnet.slice("magnet:?".length);
  for (const param of query.split("&")) {
    const eq = param.indexOf("=");
    if (eq < 0) {
      continue;
    }
    if (param.slice(0, eq).toLowerCase() !== "xt") {
      continue;
    }
    const value = decodeSafe(param.slice(eq + 1));
    if (value === null) {
      continue;
    }
    if (value.startsWith("urn:btih:")) {
      const hash = value.slice("urn:btih:".length);
      if (/^[0-9a-fA-F]{40}$/.test(hash) || /^[A-Za-z2-7]{32}$/.test(hash)) {
        return true;
      }
    } else if (value.startsWith("urn:btmh:1220")) {
      const hash = value.slice("urn:btmh:1220".length);
      if (/^[0-9a-fA-F]{64}$/.test(hash)) {
        return true;
      }
    }
  }
  return false;
}

export const magnetSchema = z
  .string()
  .trim()
  .min(1, MAGNET_ERROR)
  .max(MAGNET_MAX_BYTES, MAGNET_ERROR)
  .refine((value) => value.startsWith("magnet:?"), MAGNET_ERROR)
  .refine(magnetHasValidHash, MAGNET_ERROR);

/** §7.3 / §5.5.3: the optional display name, at most 255 characters. */
export const displayNameSchema = z.string().trim().max(255, "Use at most 255 characters.");

/** §7.3: an absolute http(s) URL, at most 8 KiB. */
export const webUrlSchema = z
  .string()
  .trim()
  .min(1, "Enter a link.")
  .max(WEB_URL_MAX_BYTES, "This link is too long.")
  .refine((value) => {
    try {
      const url = new URL(value);
      return url.protocol === "http:" || url.protocol === "https:";
    } catch {
      return false;
    }
  }, "Enter a full http(s) link.");
