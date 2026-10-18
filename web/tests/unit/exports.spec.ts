import { describe, expect, it } from "vitest";
import { formatAria2, formatUrlList, sanitizeBaseName, sanitizeOutPath } from "@/lib/exports";

describe("formatAria2", () => {
  it("writes one entry per line with an indented out=", () => {
    expect(
      formatAria2([
        { url: "https://example.com/a", out: "Sintel/Sintel.mp4" },
        { url: "https://example.com/b", out: "Sintel/Sintel.srt" },
      ]),
    ).toBe(
      "https://example.com/a\n  out=Sintel/Sintel.mp4\nhttps://example.com/b\n  out=Sintel/Sintel.srt",
    );
  });

  it("returns an empty string for no entries", () => {
    expect(formatAria2([])).toBe("");
  });
});

describe("formatUrlList", () => {
  it("joins URLs with newlines", () => {
    expect(formatUrlList(["https://a", "https://b"])).toBe("https://a\nhttps://b");
  });

  it("returns an empty string for no URLs", () => {
    expect(formatUrlList([])).toBe("");
  });
});

describe("sanitizeBaseName", () => {
  it("keeps the base name and drops the path", () => {
    expect(sanitizeBaseName("Sintel/Sintel.mp4")).toBe("Sintel.mp4");
    expect(sanitizeBaseName("C:\\Sintel\\Sintel.mp4")).toBe("Sintel.mp4");
  });

  it("removes control characters", () => {
    expect(sanitizeBaseName("bad\u0000name.mp4")).toBe("badname.mp4");
  });
});

describe("sanitizeOutPath", () => {
  it("keeps normal segments", () => {
    expect(sanitizeOutPath("Sintel/Sintel.mp4")).toBe("Sintel/Sintel.mp4");
  });

  it("drops .. path segments", () => {
    expect(sanitizeOutPath("../Sintel/Sintel.mp4")).toBe("Sintel/Sintel.mp4");
    expect(sanitizeOutPath("a/../b/../../c.txt")).toBe("a/b/c.txt");
  });

  it("drops empty segments and control characters", () => {
    expect(sanitizeOutPath("//Sintel\u0001/Sintel.mp4")).toBe("Sintel/Sintel.mp4");
  });

  it("treats backslashes as separators and drops . and .. there too", () => {
    expect(sanitizeOutPath("..\\..\\Windows\\evil.dll")).toBe("Windows/evil.dll");
    expect(sanitizeOutPath("Sintel\\.\\Sintel.mp4")).toBe("Sintel/Sintel.mp4");
  });
});
