import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Copy, Download, File as FileIcon, Folder, Play } from "lucide-react";
import { type KeyboardEvent, useState } from "react";
import { configQueryOptions } from "@/api/queries/config";
import type { Job } from "@/api/queries/jobs";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  buildFileTree,
  type FileEntry,
  type FileTreeFolder,
  type FileTreeNode,
} from "@/lib/filetree";
import { formatDuration, formatSize } from "@/lib/format";
import { mediaInfo } from "@/lib/media";
import { stateInfo } from "@/lib/states";
import { cn } from "@/lib/utils";
import { copyBundleLink, copyFileLink, downloadBundle, downloadFile } from "./links";

function ArchiveBar({
  job,
  providerName,
  perFile,
}: {
  job: Job;
  providerName: string;
  perFile: boolean;
}) {
  const queryClient = useQueryClient();
  const bundle = job.delivery.bundle;
  if (bundle === undefined) {
    return null;
  }
  const format = bundle.format.toUpperCase();
  const label = perFile
    ? `Download as one archive: ${format} · ${formatSize(job.sizeBytes)}`
    : `${providerName} delivers this job only as one archive.`;
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border p-3">
      <p className="text-sm">
        {label}
        {!bundle.rangeSupported ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span> · resuming not supported</span>
            </TooltipTrigger>
            <TooltipContent>If the download breaks, it starts over.</TooltipContent>
          </Tooltip>
        ) : null}
        {bundle.format === "rar" ? (
          <span> · unpack with an archive tool after downloading</span>
        ) : null}
      </p>
      <div className="flex shrink-0 gap-2">
        <Button size="sm" onClick={() => void downloadBundle(queryClient, job.id)}>
          Download
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void copyBundleLink(queryClient, job.id)}
        >
          Copy link
        </Button>
      </div>
    </div>
  );
}

function FileTree({
  nodes,
  job,
  showActions,
  onPlay,
}: {
  nodes: FileTreeNode[];
  job: Job;
  showActions: boolean;
  onPlay: (file: FileEntry) => void;
}) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => {
    const set = new Set<string>();
    const walk = (list: FileTreeNode[]) => {
      for (const node of list) {
        if (node.kind === "folder") {
          if (node.defaultExpanded) {
            set.add(node.path);
          }
          walk(node.children);
        }
      }
    };
    walk(nodes);
    return set;
  });

  const toggle = (path: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  };

  const onFolderKeyDown = (event: KeyboardEvent<HTMLDivElement>, path: string, isOpen: boolean) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      toggle(path);
    } else if (event.key === "ArrowRight" && !isOpen) {
      event.preventDefault();
      toggle(path);
    } else if (event.key === "ArrowLeft" && isOpen) {
      event.preventDefault();
      toggle(path);
    }
  };

  const renderFile = (node: Extract<FileTreeNode, { kind: "file" }>, depth: number) => {
    const media = mediaInfo(node.path);
    return (
      <div
        key={node.id}
        role="treeitem"
        tabIndex={0}
        className="hover:bg-accent flex h-7 items-center gap-2 px-2 text-sm"
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        <FileIcon className="text-muted-foreground size-3.5 shrink-0" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate" title={node.path}>
          {node.name}
        </span>
        <span className="text-muted-foreground tabular-nums">{formatSize(node.sizeBytes)}</span>
        {showActions ? (
          <span className="flex shrink-0 items-center gap-0.5">
            {media.element !== null ? (
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={`Play ${node.name}`}
                onClick={() => onPlay({ id: node.id, path: node.path, sizeBytes: node.sizeBytes })}
              >
                <Play className="size-3" aria-hidden="true" />
              </Button>
            ) : null}
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={`Copy link for ${node.name}`}
              onClick={() => void copyFileLink(queryClient, job.id, node.id)}
            >
              <Copy className="size-3" aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={`Download ${node.name}`}
              onClick={() => void downloadFile(queryClient, job.id, node.id)}
            >
              <Download className="size-3" aria-hidden="true" />
            </Button>
          </span>
        ) : null}
      </div>
    );
  };

  const renderFolder = (node: FileTreeFolder, depth: number) => {
    const isOpen = expanded.has(node.path);
    return (
      <div key={node.path}>
        <div
          role="treeitem"
          aria-expanded={isOpen}
          tabIndex={0}
          className="hover:bg-accent flex h-7 cursor-pointer items-center gap-2 px-2 text-sm"
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={() => toggle(node.path)}
          onKeyDown={(event) => onFolderKeyDown(event, node.path, isOpen)}
        >
          <ChevronRight
            className={cn("text-muted-foreground size-3.5 shrink-0", isOpen && "rotate-90")}
            aria-hidden="true"
          />
          <Folder className="text-muted-foreground size-3.5 shrink-0" aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate" title={node.path}>
            {node.name}
          </span>
          <span className="text-muted-foreground tabular-nums">{formatSize(node.sizeBytes)}</span>
        </div>
        {isOpen ? (
          <div>
            {node.children.map((child) =>
              child.kind === "folder"
                ? renderFolder(child, depth + 1)
                : renderFile(child, depth + 1),
            )}
          </div>
        ) : null}
      </div>
    );
  };

  return (
    <div role="tree" aria-label="Files">
      {nodes.map((node) => (node.kind === "folder" ? renderFolder(node, 0) : renderFile(node, 0)))}
    </div>
  );
}

/** The Files tab (§5.6.5): archive bar, folder tree and the link-expiry note. */
export function FilesTab({
  job,
  providerName,
  onPlay,
}: {
  job: Job;
  providerName: string;
  onPlay: (file: FileEntry) => void;
}) {
  const { data: config } = useQuery(configQueryOptions);
  const ready = job.state === "ready";
  const active = stateInfo(job.state).group === "active";
  const perFile = job.delivery.perFile;
  const bundle = job.delivery.bundle;
  const showActions = ready && perFile;

  if (job.files.length === 0 && active) {
    return (
      <div className="text-muted-foreground px-6 py-4 text-sm">
        The file list appears once {providerName} knows it.
      </div>
    );
  }

  const tree = buildFileTree(job.files);

  return (
    <div className="space-y-3 px-6 py-4">
      {ready && bundle !== undefined ? (
        <ArchiveBar job={job} providerName={providerName} perFile={perFile} />
      ) : null}
      {!ready ? (
        <p className="text-muted-foreground text-sm">
          Downloads are available once the job is ready.
        </p>
      ) : null}
      {tree.length > 0 ? (
        <FileTree nodes={tree} job={job} showActions={showActions} onPlay={onPlay} />
      ) : null}
      {config ? (
        <p className="text-muted-foreground border-t pt-3 text-xs">
          Links expire {formatDuration(config.signedLinkTTLSeconds)} after they are made. They work
          without logging in: share them carefully.
        </p>
      ) : null}
    </div>
  );
}
