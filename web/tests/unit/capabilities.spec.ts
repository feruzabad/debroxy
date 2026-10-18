import { describe, expect, it } from "vitest";
import { capabilityLabel, isCapabilityAvailable } from "@/lib/capabilities";

describe("capabilityLabel", () => {
  it.each([
    ["SubmitMagnet", "Add magnet links"],
    ["SubmitTorrentFile", "Add .torrent files"],
    ["SubmitWebURL", "Add web links"],
    ["SubmitNZBFile", "Add NZB files"],
    ["SubmitNZBURL", "Add NZB links"],
    ["PerFileDelivery", "Single-file downloads"],
    ["BundleDelivery", "Archive downloads"],
    ["CachedCheck", "Cached check"],
    ["Webhooks", "Webhooks"],
    ["ProviderDedup", "Deduplicates content"],
  ])("%s → %s", (name, expected) => {
    expect(capabilityLabel(name)).toBe(expected);
  });

  it("returns the raw name for an unknown capability", () => {
    expect(capabilityLabel("SomethingElse")).toBe("SomethingElse");
  });
});

describe("isCapabilityAvailable", () => {
  it("is available when enabled and available", () => {
    expect(isCapabilityAvailable({ enabled: true, state: "available" })).toBe(true);
  });

  it("is available when enabled and unverified", () => {
    expect(isCapabilityAvailable({ enabled: true, state: "unverified" })).toBe(true);
  });

  it("is available when enabled with no state", () => {
    expect(isCapabilityAvailable({ enabled: true })).toBe(true);
  });

  it("is unavailable when the state is unavailable", () => {
    expect(isCapabilityAvailable({ enabled: true, state: "unavailable" })).toBe(false);
  });

  it("is unavailable when not enabled", () => {
    expect(isCapabilityAvailable({ enabled: false, state: "available" })).toBe(false);
  });
});
