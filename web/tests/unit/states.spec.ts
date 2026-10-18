import { describe, expect, it } from "vitest";
import { expiryInfo, failureText, sharedSlotInfo, stateInfo } from "@/lib/states";

describe("stateInfo", () => {
  it.each([
    ["submitting", "Adding", "active", "loader-circle", "--state-pending"],
    ["queued", "Queued", "active", "clock", "--state-pending"],
    ["resolving_metadata", "Finding files", "active", "search", "--state-pending"],
    ["awaiting_file_selection", "Selecting files", "active", "list-checks", "--state-pending"],
    ["downloading", "Downloading", "active", "arrow-down-circle", "--state-active"],
    ["processing", "Processing", "active", "cog", "--state-active"],
    ["ready", "Ready", "ready", "check-circle-2", "--state-ready"],
    ["failed", "Failed", "problems", "x-circle", "--state-failed"],
    ["gone", "Gone", "problems", "circle-off", "--state-failed"],
    ["deleting", "Removing", "problems", "trash-2", "--state-muted"],
    ["deleted", "Removed", "problems", "trash", "--state-muted"],
  ])("%s maps to %s/%s/%s/%s", (state, label, group, icon, colour) => {
    const info = stateInfo(state, { provider: "TorBox" });
    expect(info.label).toBe(label);
    expect(info.group).toBe(group);
    expect(info.icon).toBe(icon);
    expect(info.colour).toBe(colour);
  });

  it("substitutes the provider into tooltips", () => {
    expect(stateInfo("queued", { provider: "TorBox" }).tooltip).toBe("Waiting at TorBox to start.");
  });

  it("uses the failure text as the failed tooltip", () => {
    const info = stateInfo("failed", { provider: "TorBox", failureReason: "provider-rejected" });
    expect(info.tooltip).toBe("TorBox refused it.");
  });

  it("applies the stalled rule to active states only", () => {
    const active = stateInfo("downloading", { provider: "TorBox", stalled: true });
    expect(active.label).toBe("Downloading · stalled");
    expect(active.colour).toBe("--state-warning");
    expect(active.tooltip).toBe("No progress for a while.");
    expect(active.stalled).toBe(true);
  });

  it("ignores stalled for ready jobs", () => {
    const ready = stateInfo("ready", { stalled: true });
    expect(ready.label).toBe("Ready");
    expect(ready.colour).toBe("--state-ready");
    expect(ready.stalled).toBe(false);
  });

  it("falls back to a title-cased label for unknown states", () => {
    const info = stateInfo("banana");
    expect(info.label).toBe("Banana");
    expect(info.group).toBe("active");
    expect(info.icon).toBe("help-circle");
    expect(info.colour).toBe("--state-muted");
    expect(info.stalled).toBe(false);
  });

  it("title-cases snake_case unknown states", () => {
    expect(stateInfo("being_uploaded").label).toBe("Being Uploaded");
  });
});

describe("failureText", () => {
  it.each([
    ["provider-rejected", "TorBox", "TorBox refused it."],
    ["provider-failed", "TorBox", "TorBox couldn't finish it."],
    ["submission-lost", "TorBox", "It never showed up at TorBox."],
    ["merged-into", "TorBox", "TorBox merged it into another item you already have."],
    [
      "merged-into-refused",
      "TorBox",
      "TorBox merged it into an item debroxy couldn't attach you to.",
    ],
    ["blocked-by-deletion", "TorBox", "The same content was being deleted when this arrived."],
  ])("%s → %s", (reason, provider, expected) => {
    expect(failureText(reason, provider).text).toBe(expected);
  });

  it("uses the generic text for a null reason", () => {
    const result = failureText(null, "TorBox");
    expect(result.text).toBe("It failed.");
    expect(result.action).toBe("Release it and try again.");
  });

  it("quotes an unknown code", () => {
    expect(failureText("mystery", "TorBox").text).toBe("It failed (mystery).");
  });

  it("carries the recommended action", () => {
    expect(failureText("provider-rejected", "TorBox").action).toBe(
      "Check the source, or try another provider.",
    );
  });
});

describe("expiryInfo", () => {
  const now = Date.UTC(2026, 9, 13, 12, 0, 0);
  const at = (ms: number) => new Date(now + ms).toISOString();

  it("returns none for null", () => {
    expect(expiryInfo(null, now)).toEqual({ level: "none", text: null });
  });

  it("returns expired in the past", () => {
    expect(expiryInfo(at(-1000), now).level).toBe("expired");
  });

  it("says 'expired' in the past", () => {
    expect(expiryInfo(at(-1000), now)).toEqual({ level: "expired", text: "expired" });
  });

  it("is red under an hour", () => {
    expect(expiryInfo(at(30 * 60 * 1000), now).level).toBe("red");
  });

  it("is amber at exactly 1 hour", () => {
    expect(expiryInfo(at(3600 * 1000), now).level).toBe("amber");
  });

  it("is amber under a day", () => {
    expect(expiryInfo(at(2 * 3600 * 1000), now).level).toBe("amber");
  });

  it("is muted at exactly 24 hours", () => {
    expect(expiryInfo(at(24 * 3600 * 1000), now).level).toBe("muted");
  });

  it("is muted beyond a day", () => {
    expect(expiryInfo(at(2 * 86400 * 1000), now).level).toBe("muted");
  });
});

describe("sharedSlotInfo", () => {
  it("shows both badges when shared and charged", () => {
    expect(sharedSlotInfo(true, true)).toEqual({
      badges: ["⇄ shared", "counts toward quota"],
      tooltip: "Others also use this; it counts toward your quota because you added it first.",
    });
  });

  it("shows only the shared badge", () => {
    expect(sharedSlotInfo(true, false).badges).toEqual(["⇄ shared"]);
    expect(sharedSlotInfo(true, false).tooltip).toBe(
      "Others also use this; it counts toward someone else's quota while they keep it.",
    );
  });

  it("shows only the quota badge", () => {
    expect(sharedSlotInfo(false, true)).toEqual({
      badges: ["counts toward quota"],
      tooltip: "This uses one of your slots.",
    });
  });

  it("shows nothing for neither", () => {
    expect(sharedSlotInfo(false, false)).toEqual({ badges: [], tooltip: null });
  });
});
