import type { Job } from "@/api/queries/jobs";
import { ProgressCell } from "@/components/app/progress-cell";
import { jobStateInfo, StateBadge } from "@/components/app/state-badge";
import { buildFileTree, type FileTreeNode } from "@/lib/filetree";
import { formatExactTime, formatRelativeTime, formatSize } from "@/lib/format";
import { expiryInfo } from "@/lib/states";

function FileTree({ nodes, depth }: { nodes: FileTreeNode[]; depth: number }) {
  return (
    <>
      {nodes.map((node) => {
        if (node.kind === "file") {
          return (
            <div
              key={node.path}
              className="flex justify-between gap-4 text-sm"
              style={{ paddingLeft: depth * 16 }}
            >
              <span className="truncate">{node.name}</span>
              <span className="text-muted-foreground tabular-nums">
                {formatSize(node.sizeBytes)}
              </span>
            </div>
          );
        }
        return (
          <div key={node.path}>
            <div
              className="flex justify-between gap-4 text-sm font-medium"
              style={{ paddingLeft: depth * 16 }}
            >
              <span className="truncate">{node.name}/</span>
              <span className="text-muted-foreground tabular-nums">
                {formatSize(node.sizeBytes)}
              </span>
            </div>
            <FileTree nodes={node.children} depth={depth + 1} />
          </div>
        );
      })}
    </>
  );
}

/** The read-only inline detail row for another user's job (§5.12). */
export function AdminJobDetail({ job, providerName }: { job: Job; providerName: string }) {
  const info = jobStateInfo(job, providerName);
  const expiry = expiryInfo(job.expiresAt);
  const tree = buildFileTree(job.files);

  return (
    <div className="bg-muted/30 space-y-2 border-t px-3 py-3">
      <div className="flex items-center gap-2">
        <span className="font-medium">{job.displayName}</span>
        <StateBadge info={info} />
      </div>
      <p className="text-muted-foreground text-xs">
        {providerName} · {formatSize(job.sizeBytes)} · added {formatRelativeTime(job.createdAt)}
        {expiry.text !== null ? ` · ${expiry.text}` : ""} · owned by{" "}
        {job.ownerUsername ?? "unknown"}
      </p>
      <ProgressCell job={job} provider={providerName} />
      <div className="text-muted-foreground text-xs">Added {formatExactTime(job.createdAt)}</div>
      <div className="max-h-64 space-y-1 overflow-auto rounded-md border p-2">
        <FileTree nodes={tree} depth={0} />
      </div>
    </div>
  );
}
