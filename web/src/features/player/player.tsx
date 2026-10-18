import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { keys } from "@/api/keys";
import { issueLink, type SignedLink } from "@/api/links";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { copyText } from "@/lib/browser";
import type { FileEntry } from "@/lib/filetree";
import { formatExactTime } from "@/lib/format";
import { mediaInfo } from "@/lib/media";

const TEN_MIN_MS = 10 * 60_000;

type PlayerState =
  | { kind: "loading" }
  | { kind: "ready"; link: SignedLink }
  | { kind: "cannot-play"; link: SignedLink | null }
  | { kind: "too-many-streams"; retryAfter: number };

/** Issues a link, re-issuing once when it expires within 10 minutes (§5.7). */
async function freshLink(
  queryClient: QueryClient,
  jobId: string,
  fileId: string,
): Promise<SignedLink> {
  let link = await issueLink(queryClient, jobId, { fileId });
  if (Date.parse(link.expiresAt) - Date.now() < TEN_MIN_MS) {
    queryClient.removeQueries({ queryKey: keys.links(jobId, `file:${fileId}`) });
    link = await issueLink(queryClient, jobId, { fileId });
  }
  return link;
}

function parseRetryAfter(response: Response): number {
  const header = response.headers.get("retry-after");
  if (header !== null && /^\d+$/.test(header)) {
    return Number.parseInt(header, 10);
  }
  return 5;
}

/** The error slug of a HEAD body, when the server sends a JSON error (§7.2). */
async function readErrorSlug(response: Response): Promise<string | null> {
  try {
    const body = (await response.json()) as { slug?: unknown };
    return typeof body.slug === "string" ? body.slug : null;
  } catch {
    return null;
  }
}

/** The media player modal (§5.7): a Signed Link streamed in a <video>/<audio>. */
export function Player({
  jobId,
  file,
  open,
  onOpenChange,
}: {
  jobId: string;
  file: FileEntry;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const media = mediaInfo(file.path);
  const [state, setState] = useState<PlayerState>({ kind: "loading" });
  const reissuedRef = useRef(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const audioRef = useRef<HTMLAudioElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setState({ kind: "loading" });
    reissuedRef.current = false;
    void freshLink(queryClient, jobId, file.id)
      .then((link) => {
        if (!cancelled) {
          setState({ kind: "ready", link });
        }
      })
      .catch(() => {
        if (!cancelled) {
          setState({ kind: "cannot-play", link: null });
        }
      });
    return () => {
      cancelled = true;
      const element = videoRef.current ?? audioRef.current;
      if (element !== null) {
        element.pause();
        element.removeAttribute("src");
        element.load();
      }
    };
  }, [open, jobId, file.id, queryClient]);

  // A 503 keeps retrying after the server's retryAfter (§5.7).
  useEffect(() => {
    if (state.kind !== "too-many-streams") {
      return;
    }
    const id = window.setTimeout(() => {
      setState({ kind: "loading" });
      void freshLink(queryClient, jobId, file.id)
        .then((link) => setState({ kind: "ready", link }))
        .catch(() => setState({ kind: "cannot-play", link: null }));
    }, state.retryAfter * 1000);
    return () => window.clearTimeout(id);
  }, [state, queryClient, jobId, file.id]);

  const onMediaError = async () => {
    const link = state.kind === "ready" ? state.link : null;
    if (link === null) {
      setState({ kind: "cannot-play", link: null });
      return;
    }
    try {
      const response = await fetch(link.url, { method: "HEAD" });
      if (response.status === 503) {
        setState({ kind: "too-many-streams", retryAfter: parseRetryAfter(response) });
        return;
      }
      if (response.status === 403 && !reissuedRef.current) {
        reissuedRef.current = true;
        const slug = await readErrorSlug(response);
        if (slug === "invalid-signed-link") {
          toast.error("Link invalid. This link is broken.", { duration: 10_000 });
        }
        queryClient.removeQueries({ queryKey: keys.links(jobId, `file:${file.id}`) });
        const fresh = await freshLink(queryClient, jobId, file.id);
        setState({ kind: "ready", link: fresh });
        return;
      }
      setState({ kind: "cannot-play", link });
    } catch {
      setState({ kind: "cannot-play", link });
    }
  };

  const copyLink = async (link: SignedLink) => {
    await copyText(link.url);
    toast.success(
      `Link copied. It works for anyone who has it until ${formatExactTime(link.expiresAt)}.`,
    );
  };

  const link = state.kind === "ready" || state.kind === "cannot-play" ? state.link : null;
  const showWarning = media.playability === "maybe";
  const showSeekingNote = state.kind === "ready" && !state.link.rangeSupported;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[1200px]">
        <DialogHeader>
          <DialogTitle>{file.path.split("/").pop() ?? file.path}</DialogTitle>
        </DialogHeader>

        {showWarning ? (
          <p role="status" className="text-state-warning text-sm">
            Your browser may not be able to play {media.extension} files. If it doesn&apos;t start,
            use Download.
          </p>
        ) : null}

        {state.kind === "loading" ? (
          <div className="text-muted-foreground flex aspect-video items-center justify-center">
            Loading…
          </div>
        ) : null}

        {state.kind === "too-many-streams" ? (
          <div className="flex aspect-video flex-col items-center justify-center gap-3 text-center">
            <p className="text-state-failed">
              You have too many downloads running. Close one, or wait {state.retryAfter}s.
            </p>
          </div>
        ) : null}

        {state.kind === "cannot-play" ? (
          <div className="flex aspect-video flex-col items-center justify-center gap-3">
            <p className="text-state-failed">This file can&apos;t be played here.</p>
            {link !== null ? (
              <div className="flex gap-2">
                <Button asChild size="sm">
                  <a href={link.url}>Download instead</a>
                </Button>
                <Button variant="outline" size="sm" onClick={() => void copyLink(link)}>
                  Copy link
                </Button>
              </div>
            ) : null}
          </div>
        ) : null}

        {state.kind === "ready" ? (
          <div className="space-y-2">
            {media.element === "video" ? (
              // biome-ignore lint/a11y/useMediaCaption: subtitles are out of scope in v1 (FE-31)
              <video
                ref={videoRef}
                data-testid="player-media"
                src={state.link.url}
                preload="metadata"
                controls
                playsInline
                className="aspect-video w-full"
                onError={() => void onMediaError()}
              />
            ) : (
              // biome-ignore lint/a11y/useMediaCaption: subtitles are out of scope in v1 (FE-31)
              <audio
                ref={audioRef}
                data-testid="player-media"
                src={state.link.url}
                preload="metadata"
                controls
                className="w-full"
                onError={() => void onMediaError()}
              />
            )}
            {showSeekingNote ? (
              <DialogDescription>Seeking isn&apos;t supported for this file.</DialogDescription>
            ) : null}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
