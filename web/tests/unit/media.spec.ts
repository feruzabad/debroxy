import { describe, expect, it } from "vitest";
import { mediaInfo } from "@/lib/media";

describe("mediaInfo", () => {
  it.each([
    ["movie.mp4", "video", "native"],
    ["movie.m4v", "video", "native"],
    ["movie.webm", "video", "native"],
    ["movie.mov", "video", "native"],
    ["movie.mkv", "video", "maybe"],
    ["movie.avi", "video", "maybe"],
    ["movie.wmv", "video", "maybe"],
    ["movie.flv", "video", "maybe"],
    ["movie.mpg", "video", "maybe"],
    ["movie.mpeg", "video", "maybe"],
    ["movie.ts", "video", "maybe"],
    ["movie.m2ts", "video", "maybe"],
    ["movie.3gp", "video", "maybe"],
    ["movie.ogv", "video", "maybe"],
    ["song.mp3", "audio", "native"],
    ["song.m4a", "audio", "native"],
    ["song.aac", "audio", "native"],
    ["song.ogg", "audio", "native"],
    ["song.oga", "audio", "native"],
    ["song.opus", "audio", "native"],
    ["song.flac", "audio", "native"],
    ["song.wav", "audio", "native"],
    ["song.wma", "audio", "maybe"],
    ["song.ape", "audio", "maybe"],
    ["song.alac", "audio", "maybe"],
    ["song.aiff", "audio", "maybe"],
    ["song.dsf", "audio", "maybe"],
  ])("%s → %s/%s", (path, element, playability) => {
    expect(mediaInfo(path)).toMatchObject({ element, playability });
  });

  it("is case-insensitive", () => {
    expect(mediaInfo("MOVIE.MP4").element).toBe("video");
    expect(mediaInfo("song.FLAC").element).toBe("audio");
  });

  it("uses the base name, not the folder", () => {
    expect(mediaInfo("Subs/Folder.mp4").element).toBe("video");
  });

  it("returns none for other extensions", () => {
    expect(mediaInfo("archive.zip")).toEqual({
      element: null,
      playability: "none",
      extension: "zip",
    });
    expect(mediaInfo("notes.pdf").element).toBe(null);
  });

  it("returns none when there is no extension", () => {
    expect(mediaInfo("README")).toEqual({ element: null, playability: "none", extension: null });
  });

  it("reports the lower-cased extension", () => {
    expect(mediaInfo("movie.MKV").extension).toBe("mkv");
  });
});
