// Number and time formats (docs/frontend/revision2.md §6.6).

const SIZE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB"] as const;

const sizeFormat = new Intl.NumberFormat("en", { maximumFractionDigits: 2, useGrouping: false });

function formatSizeNumber(value: number): string {
  // 3 significant digits: ≥ 100 → 0 decimals, ≥ 10 → 1, else 2.
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return sizeFormat.format(Number(value.toFixed(digits)));
}

/** IEC size (base 1024): `754 MiB`, `12.3 GiB`, `2.15 KiB`; `0` → `"0 B"`. */
export function formatSize(bytes: number): string {
  if (bytes === 0) {
    return "0 B";
  }
  const sign = bytes < 0 ? -1 : 1;
  let value = Math.abs(bytes);
  let unit = 0;
  while (value >= 1024 && unit < SIZE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${formatSizeNumber(sign * value)} ${SIZE_UNITS[unit]}`;
}

/** Speed (bytes/s): the size format plus `/s`; `0` → `"—"`. */
export function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) {
    return "—";
  }
  return `${formatSize(bytesPerSecond)}/s`;
}

function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

/** ETA (seconds): `41s`, `12m 05s`, `3h 12m`, `2d 4h`; null → `"—"`; negatives clamp to 0. */
export function formatEta(etaSeconds: number | null | undefined): string {
  if (etaSeconds == null) {
    return "—";
  }
  const total = Math.max(0, Math.floor(etaSeconds));
  const seconds = total % 60;
  const minutes = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600) % 24;
  const days = Math.floor(total / 86400);
  if (total < 60) {
    return `${total}s`;
  }
  if (total < 3600) {
    return `${minutes}m ${pad2(seconds)}s`;
  }
  if (total < 86400) {
    return `${hours}h ${pad2(minutes)}m`;
  }
  return `${days}d ${hours}h`;
}

/** Progress (`0..1`) → one-decimal percent, clamped to 0–100: `62.4%`. */
export function formatProgress(progress: number): string {
  const clamped = Math.min(100, Math.max(0, progress * 100));
  return `${clamped.toFixed(1)}%`;
}

const relativeFormat = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

const RELATIVE_UNITS: ReadonlyArray<{ name: Intl.RelativeTimeFormatUnit; seconds: number }> = [
  { name: "year", seconds: 365 * 86400 },
  { name: "month", seconds: 30 * 86400 },
  { name: "day", seconds: 86400 },
  { name: "hour", seconds: 3600 },
  { name: "minute", seconds: 60 },
  { name: "second", seconds: 1 },
];

/** Relative time with the largest unit (`s`, `min`, `h`, `d`, `mo`, `y`). */
export function formatRelativeTime(date: string | number | Date, now: number = Date.now()): string {
  const target = typeof date === "number" ? date : new Date(date).getTime();
  const diffSeconds = Math.round((target - now) / 1000);
  for (const unit of RELATIVE_UNITS) {
    if (Math.abs(diffSeconds) >= unit.seconds || unit.name === "second") {
      return relativeFormat.format(Math.round(diffSeconds / unit.seconds), unit.name);
    }
  }
  return relativeFormat.format(0, "second");
}

const exactFormat = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

/** Exact time in the browser locale and time zone (tooltips, banners). */
export function formatExactTime(date: string | number | Date): string {
  return exactFormat.format(new Date(date));
}

/** Countdown: `m:ss`, or `h:mm:ss` from 1 h (e.g. `0:42`). */
export function formatCountdown(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const sec = total % 60;
  const min = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600);
  if (hours >= 1) {
    return `${hours}:${pad2(min)}:${pad2(sec)}`;
  }
  return `${min}:${pad2(sec)}`;
}

const DURATION_UNITS: ReadonlyArray<{ label: string; seconds: number }> = [
  { label: "d", seconds: 86400 },
  { label: "h", seconds: 3600 },
  { label: "m", seconds: 60 },
  { label: "s", seconds: 1 },
];

/** Durations from config (`signedLinkTTLSeconds`): the largest whole unit, e.g. `"6 h"`. */
export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  for (const unit of DURATION_UNITS) {
    if (total >= unit.seconds && total % unit.seconds === 0) {
      return `${total / unit.seconds} ${unit.label}`;
    }
  }
  return `${total} s`;
}
