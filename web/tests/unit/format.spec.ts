import { describe, expect, it } from "vitest";
import {
  formatCountdown,
  formatDuration,
  formatEta,
  formatExactTime,
  formatProgress,
  formatRelativeTime,
  formatSize,
  formatSpeed,
} from "@/lib/format";

const MiB = 1024 * 1024;
const GiB = 1024 * MiB;

describe("formatSize", () => {
  it.each([
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1 KiB"],
    [2.15 * 1024, "2.15 KiB"],
    [754 * MiB, "754 MiB"],
    [12.3 * GiB, "12.3 GiB"],
    [1024 * GiB, "1 TiB"],
  ])("formats %d bytes as %s", (bytes, expected) => {
    expect(formatSize(bytes)).toBe(expected);
  });
});

describe("formatSpeed", () => {
  it("shows an em dash for zero", () => {
    expect(formatSpeed(0)).toBe("—");
  });

  it("formats a non-zero speed with /s", () => {
    expect(formatSpeed(12.3 * MiB)).toBe("12.3 MiB/s");
  });
});

describe("formatEta", () => {
  it.each([
    [null, "—"],
    [undefined, "—"],
    [41, "41s"],
    [12 * 60 + 5, "12m 05s"],
    [3 * 3600 + 12 * 60, "3h 12m"],
    [2 * 86400 + 4 * 3600, "2d 4h"],
    [0, "0s"],
    [-5, "0s"],
  ])("formats %s as %s", (eta, expected) => {
    expect(formatEta(eta as number | null | undefined)).toBe(expected);
  });
});

describe("formatProgress", () => {
  it.each([
    [0.624, "62.4%"],
    [0, "0.0%"],
    [1, "100.0%"],
    [1.5, "100.0%"],
    [-0.1, "0.0%"],
  ])("formats %s as %s", (progress, expected) => {
    expect(formatProgress(progress)).toBe(expected);
  });
});

describe("formatRelativeTime", () => {
  const now = Date.UTC(2026, 9, 13, 12, 0, 0);

  it.each([
    [0, "now"],
    [-2 * 3600 * 1000, "2 hours ago"],
    [5 * 86400 * 1000, "in 5 days"],
    [-86400 * 1000, "yesterday"],
    [-3 * 60 * 1000, "3 minutes ago"],
    [60 * 86400 * 1000, "in 2 months"],
    [400 * 86400 * 1000, "next year"],
    [-400 * 86400 * 1000, "last year"],
  ])("formats a %d ms offset as %s", (offset, expected) => {
    expect(formatRelativeTime(now + offset, now)).toBe(expected);
  });
});

describe("formatExactTime", () => {
  it("formats the exact time in the default locale", () => {
    expect(formatExactTime("2026-10-13T14:35:00Z")).toBe("Oct 13, 2026, 2:35 PM");
  });
});

describe("formatCountdown", () => {
  it.each([
    [42, "0:42"],
    [10 * 60 + 5, "10:05"],
    [3600, "1:00:00"],
    [3600 + 2 * 60 + 3, "1:02:03"],
    [0, "0:00"],
    [-7, "0:00"],
  ])("formats %d seconds as %s", (seconds, expected) => {
    expect(formatCountdown(seconds)).toBe(expected);
  });
});

describe("formatDuration", () => {
  it.each([
    [21600, "6 h"],
    [86400, "1 d"],
    [120, "2 m"],
    [90, "90 s"],
    [30, "30 s"],
    [0, "0 s"],
  ])("formats %d seconds as %s", (seconds, expected) => {
    expect(formatDuration(seconds)).toBe(expected);
  });
});
