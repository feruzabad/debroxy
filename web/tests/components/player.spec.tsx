import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Player } from "@/features/player/player";
import { type SignedLink, signedLinkFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";

type PlayerFile = { id: string; path: string; sizeBytes: number };

function playerElement(file: PlayerFile, open = true) {
  return <Player jobId="job-1" file={file} open={open} onOpenChange={() => {}} />;
}

function renderPlayer(file: PlayerFile, links: SignedLink[] = [signedLinkFixture()]) {
  const queryClient = new QueryClient();
  let issued = 0;
  server.use(
    http.post("/api/v1/jobs/job-1/links", () => {
      const link = links[Math.min(issued, links.length - 1)];
      issued++;
      return HttpResponse.json(link);
    }),
  );
  const view = render(
    <QueryClientProvider client={queryClient}>{playerElement(file)}</QueryClientProvider>,
  );
  return {
    ...view,
    issued: () => issued,
    close: () =>
      view.rerender(
        <QueryClientProvider client={queryClient}>
          {playerElement(file, false)}
        </QueryClientProvider>,
      ),
  };
}

const mp4 = { id: "f1", path: "movie.mp4", sizeBytes: 1000 };

async function failPlayback(): Promise<void> {
  const video = await screen.findByTestId("player-media");
  fireEvent.error(video);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("player", () => {
  it("warns for media types the browser may not decode", async () => {
    renderPlayer({ id: "f1", path: "movie.mkv", sizeBytes: 1000 });
    expect(
      await screen.findByText(
        "Your browser may not be able to play mkv files. If it doesn't start, use Download.",
      ),
    ).toBeInTheDocument();
  });

  it("shows no warning for native media types", async () => {
    renderPlayer(mp4);
    await screen.findByRole("dialog");
    expect(screen.queryByText(/Your browser may not be able to play/)).not.toBeInTheDocument();
  });

  it("streams the Signed Link with metadata preload", async () => {
    renderPlayer(mp4);
    const video = await screen.findByTestId("player-media");
    expect(video.tagName).toBe("VIDEO");
    expect(video).toHaveAttribute("src", signedLinkFixture().url);
    expect(video).toHaveAttribute("preload", "metadata");
  });

  it("uses an audio element for audio files", async () => {
    renderPlayer({ id: "f1", path: "song.mp3", sizeBytes: 1000 });
    expect((await screen.findByTestId("player-media")).tagName).toBe("AUDIO");
  });

  it("notes when seeking isn't supported", async () => {
    renderPlayer(mp4, [signedLinkFixture({ rangeSupported: false })]);
    expect(await screen.findByText("Seeking isn't supported for this file.")).toBeInTheDocument();
  });

  it("re-issues a link that expires within 10 minutes", async () => {
    const soon = signedLinkFixture({
      url: "http://localhost/dl/old/movie.mp4",
      expiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
    });
    const fresh = signedLinkFixture({ url: "http://localhost/dl/new/movie.mp4" });
    const player = renderPlayer(mp4, [soon, fresh]);
    expect(await screen.findByTestId("player-media")).toHaveAttribute("src", fresh.url);
    expect(player.issued()).toBe(2);
  });

  it("offers download and copy when the file can't be played", async () => {
    server.use(http.head("http://localhost/dl/*", () => new HttpResponse(null, { status: 200 })));
    renderPlayer(mp4);
    await failPlayback();
    expect(await screen.findByText("This file can't be played here.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Download instead" })).toHaveAttribute(
      "href",
      signedLinkFixture().url,
    );
    expect(screen.getByRole("button", { name: "Copy link" })).toBeInTheDocument();
  });

  it("explains too many streams and waits for Retry-After", async () => {
    server.use(
      http.head(
        "http://localhost/dl/*",
        () => new HttpResponse(null, { status: 503, headers: { "Retry-After": "7" } }),
      ),
    );
    renderPlayer(mp4);
    await failPlayback();
    expect(
      await screen.findByText("You have too many downloads running. Close one, or wait 7s."),
    ).toBeInTheDocument();
  });

  it("re-issues an expired link once", async () => {
    let heads = 0;
    server.use(
      http.head("http://localhost/dl/*", () => {
        heads++;
        return new HttpResponse(null, { status: 403 });
      }),
    );
    const fresh = signedLinkFixture({ url: "http://localhost/dl/new/movie.mp4" });
    const player = renderPlayer(mp4, [signedLinkFixture(), fresh]);
    await failPlayback();
    expect(await screen.findByTestId("player-media")).toHaveAttribute("src", fresh.url);
    expect(player.issued()).toBe(2);
    // A second failure does not re-issue again.
    await failPlayback();
    expect(await screen.findByText("This file can't be played here.")).toBeInTheDocument();
    expect(player.issued()).toBe(2);
    expect(heads).toBe(2);
  });

  it("unloads the media element on close", async () => {
    const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
    const load = vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    const player = renderPlayer(mp4);
    const video = await screen.findByTestId("player-media");
    player.close();
    expect(pause).toHaveBeenCalled();
    expect(load).toHaveBeenCalled();
    expect(video).not.toHaveAttribute("src");
  });
});
