// Capability labels and the tab-availability rule (docs/frontend/revision2.md
// §6.5 and §5.5.3).

const CAPABILITY_LABELS: Record<string, string> = {
  SubmitMagnet: "Add magnet links",
  SubmitTorrentFile: "Add .torrent files",
  SubmitWebURL: "Add web links",
  SubmitNZBFile: "Add NZB files",
  SubmitNZBURL: "Add NZB links",
  PerFileDelivery: "Single-file downloads",
  BundleDelivery: "Archive downloads",
  CachedCheck: "Cached check",
  Webhooks: "Webhooks",
  ProviderDedup: "Deduplicates content",
};

/** §6.5: the human label for a capability name; the raw name for unknown ones. */
export function capabilityLabel(name: string): string {
  return CAPABILITY_LABELS[name] ?? name;
}

export interface CapabilityLike {
  enabled: boolean;
  state?: "available" | "unavailable" | "unverified";
}

/** §5.5.3: a tab is available when its capability is enabled and not `unavailable`. */
export function isCapabilityAvailable(capability: CapabilityLike): boolean {
  return capability.enabled && capability.state !== "unavailable";
}
