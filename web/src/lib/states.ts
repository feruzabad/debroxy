// Domain presentation rules: states, failure reasons, expiry and the shared/
// slotCharged badges (docs/frontend/revision2.md §6.1–§6.4).

import { formatRelativeTime } from "./format";

export type JobState =
  | "submitting"
  | "queued"
  | "resolving_metadata"
  | "awaiting_file_selection"
  | "downloading"
  | "processing"
  | "ready"
  | "failed"
  | "gone"
  | "deleting"
  | "deleted";

export type StateGroup = "active" | "ready" | "problems";

export interface StateInfo {
  /** The raw state value, or the raw unknown value for a future server state. */
  state: string;
  label: string;
  group: StateGroup;
  /** lucide icon name. */
  icon: string;
  /** CSS colour token. */
  colour: string;
  /** Tooltip text, with `{provider}` already substituted. */
  tooltip: string;
  /** Whether the "· stalled" suffix and `--state-warning` colour apply. */
  stalled: boolean;
}

export interface StateRenderOptions {
  stalled?: boolean;
  /** Provider display name, substituted into tooltips. */
  provider?: string;
  failureReason?: string | null;
}

interface StateEntry {
  label: string;
  group: StateGroup;
  icon: string;
  colour: string;
  tooltip: (provider: string) => string;
}

const STATE_TABLE: Record<string, StateEntry> = {
  submitting: {
    label: "Adding",
    group: "active",
    icon: "loader-circle",
    colour: "--state-pending",
    tooltip: (p) => `debroxy is handing this to ${p}.`,
  },
  queued: {
    label: "Queued",
    group: "active",
    icon: "clock",
    colour: "--state-pending",
    tooltip: (p) => `Waiting at ${p} to start.`,
  },
  resolving_metadata: {
    label: "Finding files",
    group: "active",
    icon: "search",
    colour: "--state-pending",
    tooltip: (p) => `${p} is fetching the torrent's file list.`,
  },
  awaiting_file_selection: {
    label: "Selecting files",
    group: "active",
    icon: "list-checks",
    colour: "--state-pending",
    tooltip: (p) => `${p} is selecting all files; this resolves by itself.`,
  },
  downloading: {
    label: "Downloading",
    group: "active",
    icon: "arrow-down-circle",
    colour: "--state-active",
    tooltip: (p) => `${p} is downloading it.`,
  },
  processing: {
    label: "Processing",
    group: "active",
    icon: "cog",
    colour: "--state-active",
    tooltip: (p) => `${p} is preparing the files.`,
  },
  ready: {
    label: "Ready",
    group: "ready",
    icon: "check-circle-2",
    colour: "--state-ready",
    tooltip: () => "Ready to download.",
  },
  failed: {
    label: "Failed",
    group: "problems",
    icon: "x-circle",
    colour: "--state-failed",
    tooltip: () => "", // replaced with the §6.3 failure text
  },
  gone: {
    label: "Gone",
    group: "problems",
    icon: "circle-off",
    colour: "--state-failed",
    tooltip: (p) => `It disappeared from ${p}.`,
  },
  deleting: {
    label: "Removing",
    group: "problems",
    icon: "trash-2",
    colour: "--state-muted",
    tooltip: (p) => `debroxy is deleting it at ${p}.`,
  },
  deleted: {
    label: "Removed",
    group: "problems",
    icon: "trash",
    colour: "--state-muted",
    tooltip: (p) => `Deleted at ${p}.`,
  },
};

const ACTIVE_GROUPS: ReadonlySet<StateGroup> = new Set<StateGroup>(["active"]);

/** The §6.1 order, used for the "State" column sort (§5.3.3). Unknown states sort last. */
export const STATE_ORDER: readonly string[] = [
  "submitting",
  "queued",
  "resolving_metadata",
  "awaiting_file_selection",
  "downloading",
  "processing",
  "ready",
  "failed",
  "gone",
  "deleting",
  "deleted",
];

/** The sort index of a state; unknown (future) states come after every known one. */
export function stateOrderIndex(state: string): number {
  const index = STATE_ORDER.indexOf(state);
  return index < 0 ? STATE_ORDER.length : index;
}

function toTitleCase(value: string): string {
  const words = value
    .replace(/[_-]+/g, " ")
    .split(" ")
    .filter((w) => w.length > 0);
  if (words.length === 0) {
    return value;
  }
  return words
    .map((w) =>
      w.length === 1 ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1).toLowerCase(),
    )
    .join(" ");
}

/** The presentation of a job state, including the stalled rule and the unknown-state fallback. */
export function stateInfo(state: string, options: StateRenderOptions = {}): StateInfo {
  const { stalled = false, provider = "", failureReason } = options;
  const entry = STATE_TABLE[state];
  if (entry === undefined) {
    return {
      state,
      label: toTitleCase(state),
      group: "active",
      icon: "help-circle",
      colour: "--state-muted",
      tooltip: "",
      stalled: false,
    };
  }
  const isStalled = stalled && ACTIVE_GROUPS.has(entry.group);
  const tooltip =
    state === "failed" ? failureText(failureReason, provider).text : entry.tooltip(provider);
  return {
    state,
    label: isStalled ? `${entry.label} · stalled` : entry.label,
    group: entry.group,
    icon: entry.icon,
    colour: isStalled ? "--state-warning" : entry.colour,
    tooltip: isStalled ? "No progress for a while." : tooltip,
    stalled: isStalled,
  };
}

export interface FailureText {
  text: string;
  action: string;
}

const FAILURE_ACTIONS: Record<string, { text: (provider: string) => string; action: string }> = {
  "provider-rejected": {
    text: (p) => `${p} refused it.`,
    action: "Check the source, or try another provider.",
  },
  "provider-failed": {
    text: (p) => `${p} couldn't finish it.`,
    action: "Release it and try again later, or try another provider.",
  },
  "submission-lost": {
    text: (p) => `It never showed up at ${p}.`,
    action: "Release it and add it again.",
  },
  "merged-into": {
    text: (p) => `${p} merged it into another item you already have.`,
    action: "Your job moved there; release this one.",
  },
  "merged-into-refused": {
    text: (p) => `${p} merged it into an item debroxy couldn't attach you to.`,
    action: "Release it and add it again.",
  },
  "blocked-by-deletion": {
    text: () => "The same content was being deleted when this arrived.",
    action: "Add it again in a minute.",
  },
};

/** The §6.3 failure text plus what the user can do; covers the null and unknown cases. */
export function failureText(reason: string | null | undefined, provider: string): FailureText {
  if (reason == null) {
    return { text: "It failed.", action: "Release it and try again." };
  }
  const entry = FAILURE_ACTIONS[reason];
  if (entry !== undefined) {
    return { text: entry.text(provider), action: entry.action };
  }
  return { text: `It failed (${reason}).`, action: "Release it and try again." };
}

export type ExpiryLevel = "none" | "muted" | "amber" | "red" | "expired";

export interface ExpiryInfo {
  level: ExpiryLevel;
  /** The text to show, or null when nothing is shown. */
  text: string | null;
}

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

/** §6.4 expiry levels, driven by how far away `expiresAt` is. */
export function expiryInfo(
  expiresAt: string | number | Date | null | undefined,
  now: number = Date.now(),
): ExpiryInfo {
  if (expiresAt == null) {
    return { level: "none", text: null };
  }
  const at = new Date(expiresAt).getTime();
  const remaining = at - now;
  if (remaining <= 0) {
    return { level: "expired", text: "expired" };
  }
  if (remaining < HOUR_MS) {
    return { level: "red", text: `expires ${formatRelativeTime(at, now)}` };
  }
  if (remaining < DAY_MS) {
    return { level: "amber", text: `expires ${formatRelativeTime(at, now)}` };
  }
  return { level: "muted", text: `expires ${formatRelativeTime(at, now)}` };
}

export interface SharedSlotInfo {
  badges: string[];
  tooltip: string | null;
}

/** §6.2: the "⇄ shared" / "counts toward quota" badges and their tooltip. */
export function sharedSlotInfo(shared: boolean, slotCharged: boolean): SharedSlotInfo {
  if (shared && slotCharged) {
    return {
      badges: ["⇄ shared", "counts toward quota"],
      tooltip: "Others also use this; it counts toward your quota because you added it first.",
    };
  }
  if (shared) {
    return {
      badges: ["⇄ shared"],
      tooltip: "Others also use this; it counts toward someone else's quota while they keep it.",
    };
  }
  if (slotCharged) {
    return { badges: ["counts toward quota"], tooltip: "This uses one of your slots." };
  }
  return { badges: [], tooltip: null };
}
