// Media-type detection by file extension (docs/frontend/revision2.md §6.7).

export type MediaElement = "video" | "audio" | null;
export type MediaPlayability = "native" | "maybe" | "none";

export interface MediaInfo {
  /** The `<video>`/`<audio>` element, or null when there is no Play action. */
  element: MediaElement;
  playability: MediaPlayability;
  /** The lower-cased extension, or null when there is none. */
  extension: string | null;
}

const NATIVE_VIDEO = new Set(["mp4", "m4v", "webm", "mov"]);
const MAYBE_VIDEO = new Set([
  "mkv",
  "avi",
  "wmv",
  "flv",
  "mpg",
  "mpeg",
  "ts",
  "m2ts",
  "3gp",
  "ogv",
]);
const NATIVE_AUDIO = new Set(["mp3", "m4a", "aac", "ogg", "oga", "opus", "flac", "wav"]);
const MAYBE_AUDIO = new Set(["wma", "ape", "alac", "aiff", "dsf"]);

function extensionOf(path: string): string | null {
  const base = path.split("/").pop() ?? path;
  const dot = base.lastIndexOf(".");
  if (dot <= 0 || dot === base.length - 1) {
    return null;
  }
  return base.slice(dot + 1).toLowerCase();
}

/** §6.7: plays natively / may not play / no Play action, and the element to use. */
export function mediaInfo(path: string): MediaInfo {
  const extension = extensionOf(path);
  if (extension === null) {
    return { element: null, playability: "none", extension: null };
  }
  if (NATIVE_VIDEO.has(extension)) {
    return { element: "video", playability: "native", extension };
  }
  if (MAYBE_VIDEO.has(extension)) {
    return { element: "video", playability: "maybe", extension };
  }
  if (NATIVE_AUDIO.has(extension)) {
    return { element: "audio", playability: "native", extension };
  }
  if (MAYBE_AUDIO.has(extension)) {
    return { element: "audio", playability: "maybe", extension };
  }
  return { element: null, playability: "none", extension };
}
