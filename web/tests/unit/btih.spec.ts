import { createHash, webcrypto } from "node:crypto";
import { describe, expect, it } from "vitest";
import { locateInfo, magnetFingerprint, type SubtleDigest, torrentInfoHash } from "@/api/btih";

const subtle: SubtleDigest = {
  digest: (algorithm, data) => webcrypto.subtle.digest(algorithm, data),
};

const encoder = new TextEncoder();

function concatBytes(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

function encodeBencode(value: unknown): Uint8Array {
  if (typeof value === "number") {
    return encoder.encode(`i${value}e`);
  }
  if (value instanceof Uint8Array) {
    return concatBytes([encoder.encode(`${value.length}:`), value]);
  }
  if (typeof value === "string") {
    const bytes = encoder.encode(value);
    return concatBytes([encoder.encode(`${bytes.length}:`), bytes]);
  }
  if (Array.isArray(value)) {
    return concatBytes([encoder.encode("l"), ...value.map(encodeBencode), encoder.encode("e")]);
  }
  const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) =>
    a < b ? -1 : a > b ? 1 : 0,
  );
  const parts: Uint8Array[] = [encoder.encode("d")];
  for (const [key, item] of entries) {
    parts.push(encodeBencode(key), encodeBencode(item));
  }
  parts.push(encoder.encode("e"));
  return concatBytes(parts);
}

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i += 1) {
    bytes[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return bytes;
}

function bytesToHex(bytes: Uint8Array): string {
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

const BASE32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function toBase32(bytes: Uint8Array): string {
  let bits = 0;
  let value = 0;
  let out = "";
  for (const byte of bytes) {
    value = (value << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      out += BASE32[(value >> bits) & 31];
    }
  }
  if (bits > 0) {
    out += BASE32[(value << (5 - bits)) & 31];
  }
  return out;
}

describe("magnetFingerprint", () => {
  const hex = "7acf8fb590b2060dd9c3146ef770169d593433b0";

  it("extracts a 40-hex btih, lower-cased", () => {
    expect(magnetFingerprint(`magnet:?xt=urn:btih:${hex.toUpperCase()}&dn=foo`)).toBe(
      `btih:${hex}`,
    );
  });

  it("decodes a 32-char base32 btih to hex", () => {
    const base32 = toBase32(hexToBytes(hex));
    expect(base32).toHaveLength(32);
    expect(magnetFingerprint(`magnet:?xt=urn:btih:${base32}`)).toBe(`btih:${hex}`);
  });

  it("returns null for a btmh-only (v2) magnet", () => {
    expect(magnetFingerprint(`magnet:?xt=urn:btmh:1220${"0".repeat(64)}`)).toBeNull();
  });

  it("takes the first btih among multiple xt values", () => {
    const other = "1111111111111111111111111111111111111111";
    expect(magnetFingerprint(`magnet:?xt=urn:btih:${hex}&xt=urn:btih:${other}`)).toBe(
      `btih:${hex}`,
    );
  });

  it("skips a leading btmh and finds a following btih", () => {
    expect(magnetFingerprint(`magnet:?xt=urn:btmh:1220${"0".repeat(64)}&xt=urn:btih:${hex}`)).toBe(
      `btih:${hex}`,
    );
  });

  it.each([
    "not a magnet at all",
    "magnet:?dn=no-xt",
    `magnet:?xt=urn:btih:${"g".repeat(40)}`,
    `magnet:?xt=urn:btih:short`,
  ])("returns null for malformed input: %s", (input) => {
    expect(magnetFingerprint(input)).toBeNull();
  });
});

describe("torrent info hash", () => {
  const pieces = new Uint8Array(20).fill(7);

  function buildTorrent(info: Record<string, unknown>): { torrent: Uint8Array; info: Uint8Array } {
    return {
      torrent: encodeBencode({ announce: "http://tracker/announce", info }),
      info: encodeBencode(info),
    };
  }

  it("computes the SHA-1 of the info dict for a v1 torrent", async () => {
    const { torrent, info } = buildTorrent({
      name: "file.txt",
      length: 11,
      "piece length": 16384,
      pieces,
    });
    const expected = createHash("sha1").update(info).digest("hex");
    expect(await torrentInfoHash(torrent, subtle)).toBe(`btih:${expected}`);
  });

  it("computes the hash for a hybrid (v1+v2) torrent", async () => {
    const { torrent, info } = buildTorrent({
      name: "file.txt",
      length: 11,
      "piece length": 16384,
      pieces,
      "meta version": 2,
    });
    const expected = createHash("sha1").update(info).digest("hex");
    expect(await torrentInfoHash(torrent, subtle)).toBe(`btih:${expected}`);
  });

  it("returns null for a v2-only torrent", async () => {
    const { torrent } = buildTorrent({
      "meta version": 2,
      name: "file.txt",
      "file tree": { "file.txt": {} },
    });
    expect(await torrentInfoHash(torrent, subtle)).toBeNull();
  });

  it("reports the exact info byte span", () => {
    const { torrent, info } = buildTorrent({
      name: "file.txt",
      length: 11,
      "piece length": 16384,
      pieces,
    });
    const located = locateInfo(torrent);
    expect(located).not.toBeNull();
    expect(located?.isV2Only).toBe(false);
    if (located === null) {
      return;
    }
    expect(bytesToHex(torrent.slice(located.start, located.end))).toBe(bytesToHex(info));
  });

  it("returns null for truncated bencode", () => {
    const { torrent } = buildTorrent({
      name: "file.txt",
      length: 11,
      "piece length": 16384,
      pieces,
    });
    expect(locateInfo(torrent.slice(0, -2))).toBeNull();
  });

  it("returns null for a non-dict top level", () => {
    expect(locateInfo(encoder.encode("l1:ae"))).toBeNull();
  });

  // Golden vector, written by hand (keys out of order, info in the middle,
  // strings holding "e" and ":", nested lists of dicts, a negative integer).
  // Expected: python3 -c "import hashlib; print(hashlib.sha1(<info bytes>).hexdigest())".
  it("matches a hand-written golden vector with bencode edge cases", async () => {
    const info = concatBytes([
      encoder.encode(
        "d5:filesld6:lengthi-1e4:pathl9:a:b e.txteed6:lengthi42e4:pathl3:dir5:e:e:eeee4:name11:edge:case e12:piece lengthi16384e6:pieces20:",
      ),
      pieces,
      encoder.encode("e"),
    ]);
    const torrent = concatBytes([
      encoder.encode("d8:announce3:e:e4:info"),
      info,
      encoder.encode("7:comment4:i9ee13:creation datei1700000000ee"),
    ]);
    expect(await torrentInfoHash(torrent, subtle)).toBe(
      "btih:9dc4de30feadf9949622b0a383d7a03b9552613d",
    );
  });

  it("rejects nesting deeper than the limit", () => {
    const depth = 600;
    const nested = encoder.encode(`d4:infod4:deep${"l".repeat(depth)}${"e".repeat(depth)}ee`);
    expect(locateInfo(nested)).toBeNull();
    const shallow = encoder.encode(`d4:infod4:deep${"l".repeat(10)}${"e".repeat(10)}ee`);
    expect(locateInfo(shallow)).not.toBeNull();
  });
});
