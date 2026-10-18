// Signed Links (docs/frontend/revision2.md §5.6.6): issued on demand, cached in
// memory under ['links', jobId, key] and re-issued once they are within 60 s of
// expiring. Links never go to storage or logs (§11).

import type { QueryClient } from "@tanstack/react-query";
import { deliveryClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/delivery";
import { keys, type LinkTargetKey } from "@/api/keys";

export type SignedLink = components["schemas"]["SignedLink"];

export type LinkTarget = { fileId: string } | { bundle: true };

const EXPIRY_MARGIN_MS = 60_000;

function isFresh(links: SignedLink[] | undefined, now = Date.now()): links is SignedLink[] {
  return (
    links !== undefined &&
    links.length > 0 &&
    links.every((link) => Date.parse(link.expiresAt) - EXPIRY_MARGIN_MS > now)
  );
}

function targetKey(target: LinkTarget): LinkTargetKey {
  return "bundle" in target ? "bundle" : `file:${target.fileId}`;
}

/** One link for a file or the bundle, from the cache while fresh. */
export async function issueLink(
  queryClient: QueryClient,
  jobId: string,
  target: LinkTarget,
): Promise<SignedLink> {
  const key = keys.links(jobId, targetKey(target));
  const cached = queryClient.getQueryData<SignedLink[]>(key);
  if (isFresh(cached) && cached[0] !== undefined) {
    return cached[0];
  }
  const { data } = await deliveryClient.POST("/api/v1/jobs/{jobId}/links", {
    params: { path: { jobId } },
    body: { target },
  });
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  queryClient.setQueryData(key, [data]);
  return data;
}

/** Links for every file (or the one bundle link), each also cached per target. */
export async function issueAllLinks(
  queryClient: QueryClient,
  jobId: string,
): Promise<SignedLink[]> {
  const key = keys.links(jobId, "all");
  const cached = queryClient.getQueryData<SignedLink[]>(key);
  if (isFresh(cached)) {
    return cached;
  }
  const { data } = await deliveryClient.POST("/api/v1/jobs/{jobId}/links/all", {
    params: { path: { jobId } },
  });
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  queryClient.setQueryData(key, data.links);
  for (const link of data.links) {
    if (link.fileId !== undefined) {
      queryClient.setQueryData(keys.links(jobId, `file:${link.fileId}`), [link]);
    } else if (link.bundle === true) {
      queryClient.setQueryData(keys.links(jobId, "bundle"), [link]);
    }
  }
  return data.links;
}
