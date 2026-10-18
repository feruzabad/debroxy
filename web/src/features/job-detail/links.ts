import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { issueAllLinks, issueLink, type SignedLink } from "@/api/links";
import type { Job } from "@/api/queries/jobs";
import { copyText, downloadText } from "@/lib/browser";
import { formatAria2, formatUrlList, sanitizeBaseName, sanitizeOutPath } from "@/lib/exports";
import { formatExactTime } from "@/lib/format";

/**
 * Handles a Signed Link issuing error (§5.6.6): 409s toast and refetch the job,
 * `job-not-found` refetches so the page shows the not-found state.
 */
function handleLinkIssueError(queryClient: QueryClient, jobId: string, error: unknown): void {
  if (
    isApiError(error) &&
    (error.slug === "remote-item-not-ready" || error.slug === "target-not-available")
  ) {
    toast.error(describeError(error).title, { duration: 10_000 });
    void queryClient.invalidateQueries({ queryKey: keys.job(jobId) });
    return;
  }
  if (isApiError(error) && error.slug === "job-not-found") {
    void queryClient.invalidateQueries({ queryKey: keys.job(jobId) });
    return;
  }
  toast.error(describeError(error).title, { duration: 10_000 });
}

/** Copies one file's link to the clipboard, with the §5.6.6 expiry toast. */
export async function copyFileLink(
  queryClient: QueryClient,
  jobId: string,
  fileId: string,
): Promise<void> {
  try {
    const link = await issueLink(queryClient, jobId, { fileId });
    await copyText(link.url);
    toast.success(
      `Link copied. It works for anyone who has it until ${formatExactTime(link.expiresAt)}.`,
    );
  } catch (error) {
    handleLinkIssueError(queryClient, jobId, error);
  }
}

/** Issues a file's link and starts a browser download (§5.6.5). */
export async function downloadFile(
  queryClient: QueryClient,
  jobId: string,
  fileId: string,
): Promise<void> {
  try {
    const link = await issueLink(queryClient, jobId, { fileId });
    window.location.assign(link.url);
  } catch (error) {
    handleLinkIssueError(queryClient, jobId, error);
  }
}

/** The bundle link, issued then downloaded (§5.6.5 archive bar). */
export async function downloadBundle(queryClient: QueryClient, jobId: string): Promise<void> {
  try {
    const link = await issueLink(queryClient, jobId, { bundle: true });
    window.location.assign(link.url);
  } catch (error) {
    handleLinkIssueError(queryClient, jobId, error);
  }
}

/** The bundle link, issued then copied (§5.6.5 archive bar). */
export async function copyBundleLink(queryClient: QueryClient, jobId: string): Promise<void> {
  try {
    const link = await issueLink(queryClient, jobId, { bundle: true });
    await copyText(link.url);
    toast.success(
      `Link copied. It works for anyone who has it until ${formatExactTime(link.expiresAt)}.`,
    );
  } catch (error) {
    handleLinkIssueError(queryClient, jobId, error);
  }
}

/** "Copy all links": newline-separated URLs (§5.6.6). */
export async function copyAllLinks(queryClient: QueryClient, jobId: string): Promise<void> {
  try {
    const links = await issueAllLinks(queryClient, jobId);
    await copyText(formatUrlList(links.map((link) => link.url)));
    toast.success("Links copied.");
  } catch (error) {
    handleLinkIssueError(queryClient, jobId, error);
  }
}

/** "Download link list (.txt)" and the JDownloader variant: one URL per line. */
export async function downloadLinkList(queryClient: QueryClient, job: Job): Promise<void> {
  try {
    const links = await issueAllLinks(queryClient, job.id);
    downloadText(
      `${sanitizeBaseName(job.displayName)}.txt`,
      formatUrlList(links.map((link) => link.url)),
    );
  } catch (error) {
    handleLinkIssueError(queryClient, job.id, error);
  }
}

/** The aria2 `out=` value for a link: the file path, or the archive name. */
function aria2Out(job: Job, link: SignedLink, filePath: ReadonlyMap<string, string>): string {
  if (link.fileId !== undefined) {
    return sanitizeOutPath(filePath.get(link.fileId) ?? "");
  }
  const format = job.delivery.bundle?.format ?? "zip";
  return `${sanitizeBaseName(job.displayName)}.${format}`;
}

/** "Download for aria2 (.aria2)": one `{url}\n  out={out}` entry per link. */
export async function downloadAria2(queryClient: QueryClient, job: Job): Promise<void> {
  try {
    const links = await issueAllLinks(queryClient, job.id);
    const filePath = new Map(job.files.map((file) => [file.id, file.path]));
    const entries = links.map((link) => ({ url: link.url, out: aria2Out(job, link, filePath) }));
    downloadText(`${sanitizeBaseName(job.displayName)}.aria2`, formatAria2(entries));
  } catch (error) {
    handleLinkIssueError(queryClient, job.id, error);
  }
}
