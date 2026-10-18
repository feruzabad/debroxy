// Magnet and .torrent info-hash extraction (docs/frontend/revision2.md §5.5.4).

const HEX = "0123456789abcdef";

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function toHex(bytes: Uint8Array): string {
  let out = "";
  for (const byte of bytes) {
    out += HEX.charAt(byte >> 4) + HEX.charAt(byte & 0x0f);
  }
  return out;
}

/** RFC 4648 base32 decode (case-insensitive, padding ignored). */
function decodeBase32(input: string): Uint8Array | null {
  const text = input.replace(/=+$/, "").toUpperCase();
  const output: number[] = [];
  let bits = 0;
  let value = 0;
  for (const char of text) {
    const index = BASE32_ALPHABET.indexOf(char);
    if (index < 0) {
      return null;
    }
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      output.push((value >> bits) & 0xff);
    }
  }
  return new Uint8Array(output);
}

function decodeURIComponentSafe(value: string): string | null {
  try {
    return decodeURIComponent(value);
  } catch {
    return null;
  }
}

function fingerprintFromXt(xt: string): string | null {
  if (!xt.startsWith("urn:btih:")) {
    return null;
  }
  const hash = xt.slice("urn:btih:".length).trim();
  if (/^[0-9a-fA-F]{40}$/.test(hash)) {
    return `btih:${hash.toLowerCase()}`;
  }
  if (/^[A-Za-z2-7]{32}$/.test(hash)) {
    const bytes = decodeBase32(hash);
    if (bytes !== null && bytes.length === 20) {
      return `btih:${toHex(bytes)}`;
    }
  }
  return null;
}

/**
 * The fingerprint of a magnet URI: `"btih:" + hex` from the first `urn:btih:`
 * `xt` value. A magnet with only `urn:btmh:` (BitTorrent v2) yields null, as do
 * malformed magnets.
 */
export function magnetFingerprint(magnet: string): string | null {
  if (!magnet.startsWith("magnet:?")) {
    return null;
  }
  const query = magnet.slice("magnet:?".length);
  const xts: string[] = [];
  for (const param of query.split("&")) {
    const eq = param.indexOf("=");
    if (eq < 0) {
      continue;
    }
    if (param.slice(0, eq).toLowerCase() !== "xt") {
      continue;
    }
    const value = decodeURIComponentSafe(param.slice(eq + 1));
    if (value !== null) {
      xts.push(value);
    }
  }
  for (const xt of xts) {
    const fingerprint = fingerprintFromXt(xt);
    if (fingerprint !== null) {
      return fingerprint;
    }
  }
  return null;
}

export interface TorrentInfo {
  /** Byte offset of the first byte of the top-level `info` value. */
  start: number;
  /** Byte offset one past the last byte of the top-level `info` value. */
  end: number;
  /** True when `info` has `meta version` 2 and no `pieces` (BitTorrent v2 only). */
  isV2Only: boolean;
}

interface StringSpan {
  start: number;
  length: number;
  end: number;
}

/** Reads a byte; returns -1 out of bounds, which sorts outside every value range. */
function readByte(bytes: Uint8Array, index: number): number {
  return bytes[index] ?? -1;
}

function parseBencodeString(bytes: Uint8Array, offset: number): StringSpan | null {
  let i = offset;
  while (i < bytes.length && readByte(bytes, i) >= 0x30 && readByte(bytes, i) <= 0x39) {
    i += 1;
  }
  if (i === offset || i >= bytes.length || readByte(bytes, i) !== 0x3a) {
    return null;
  }
  let length = 0;
  for (let j = offset; j < i; j += 1) {
    length = length * 10 + (readByte(bytes, j) - 0x30);
  }
  const start = i + 1;
  const end = start + length;
  if (end > bytes.length) {
    return null;
  }
  return { start, length, end };
}

/** Nesting deeper than this is rejected, so a hostile file cannot exhaust the stack. */
const MAX_BENCODE_DEPTH = 512;

function parseBencodeValue(bytes: Uint8Array, offset: number, depth = 0): number | null {
  if (offset >= bytes.length || depth > MAX_BENCODE_DEPTH) {
    return null;
  }
  const byte = readByte(bytes, offset);
  if (byte >= 0x30 && byte <= 0x39) {
    const span = parseBencodeString(bytes, offset);
    return span === null ? null : span.end;
  }
  if (byte === 0x69) {
    // integer
    const end = bytes.indexOf(0x65, offset + 1);
    return end < 0 ? null : end + 1;
  }
  if (byte === 0x6c || byte === 0x64) {
    // list or dict
    let p = offset + 1;
    while (p < bytes.length && bytes[p] !== 0x65) {
      if (byte === 0x64) {
        const key = parseBencodeString(bytes, p);
        if (key === null) {
          return null;
        }
        p = key.end;
      }
      const end = parseBencodeValue(bytes, p, depth + 1);
      if (end === null) {
        return null;
      }
      p = end;
    }
    return p >= bytes.length ? null : p + 1;
  }
  return null;
}

function keyEquals(bytes: Uint8Array, key: StringSpan, expected: string): boolean {
  if (key.length !== expected.length) {
    return false;
  }
  for (let i = 0; i < expected.length; i += 1) {
    if (bytes[key.start + i] !== expected.charCodeAt(i)) {
      return false;
    }
  }
  return true;
}

function parseIntValue(bytes: Uint8Array, start: number, end: number): number | null {
  if (start >= bytes.length || bytes[start] !== 0x69) {
    return null;
  }
  let i = start + 1;
  let negative = false;
  if (i < end && bytes[i] === 0x2d) {
    negative = true;
    i += 1;
  }
  let value = 0;
  for (; i < end - 1; i += 1) {
    const char = readByte(bytes, i);
    if (char < 0x30 || char > 0x39) {
      return null;
    }
    value = value * 10 + (char - 0x30);
  }
  return negative ? -value : value;
}

function isV2OnlyInfo(bytes: Uint8Array, start: number, end: number): boolean {
  let hasPieces = false;
  let metaVersion: number | null = null;
  let p = start + 1;
  while (p < end && bytes[p] !== 0x65) {
    const key = parseBencodeString(bytes, p);
    if (key === null) {
      return false;
    }
    const valueEnd = parseBencodeValue(bytes, key.end);
    if (valueEnd === null) {
      return false;
    }
    if (keyEquals(bytes, key, "pieces")) {
      hasPieces = true;
    } else if (keyEquals(bytes, key, "meta version")) {
      metaVersion = parseIntValue(bytes, key.end, valueEnd);
    }
    p = valueEnd;
  }
  return metaVersion === 2 && !hasPieces;
}

/**
 * Locates the raw byte span of the top-level `info` value without re-encoding,
 * and reports whether the torrent is BitTorrent v2 only.
 */
export function locateInfo(bytes: Uint8Array): TorrentInfo | null {
  if (bytes.length === 0 || bytes[0] !== 0x64) {
    return null;
  }
  let p = 1;
  while (p < bytes.length && bytes[p] !== 0x65) {
    const key = parseBencodeString(bytes, p);
    if (key === null) {
      return null;
    }
    const valueStart = key.end;
    const valueEnd = parseBencodeValue(bytes, valueStart);
    if (valueEnd === null) {
      return null;
    }
    if (keyEquals(bytes, key, "info")) {
      return {
        start: valueStart,
        end: valueEnd,
        isV2Only: isV2OnlyInfo(bytes, valueStart, valueEnd),
      };
    }
    p = valueEnd;
  }
  return null;
}

export interface SubtleDigest {
  digest(algorithm: "SHA-1", data: ArrayBuffer): Promise<ArrayBuffer>;
}

/** The `btih:` info hash of a `.torrent` file; null for a v2-only torrent. */
export async function torrentInfoHash(
  bytes: Uint8Array,
  subtle?: SubtleDigest,
): Promise<string | null> {
  const info = locateInfo(bytes);
  if (info === null || info.isV2Only) {
    return null;
  }
  const digest = subtle ?? globalThis.crypto?.subtle;
  if (digest === undefined) {
    throw new Error("crypto.subtle is unavailable");
  }
  const span = bytes.slice(info.start, info.end);
  const hash = await digest.digest("SHA-1", span.buffer as ArrayBuffer);
  return `btih:${toHex(new Uint8Array(hash))}`;
}
