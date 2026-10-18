import { Copy } from "lucide-react";
import type { ReactNode } from "react";
import { toast } from "sonner";
import type { Job } from "@/api/queries/jobs";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { copyText } from "@/lib/browser";
import { formatEta, formatProgress, formatSpeed } from "@/lib/format";
import { failureText, stateInfo } from "@/lib/states";

const KIND_LABELS: Record<string, string> = {
  torrent: "Torrent",
  nzb: "Usenet",
  web: "Web link",
};

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[10rem_1fr] gap-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** The Details tab (§5.6.2): progress/failure, kind, job id and delivery. */
export function DetailsTab({ job, providerName }: { job: Job; providerName: string }) {
  const group = stateInfo(job.state).group;
  const failure = failureText(job.failureReason ?? null, providerName);
  const bundle = job.delivery.bundle;

  const copyJobId = async () => {
    await copyText(job.id);
    toast.success("Copied.");
  };

  return (
    <div className="space-y-6 px-6 py-4">
      {group === "active" ? (
        <div className="max-w-xl space-y-1.5">
          <div className="flex items-center gap-2">
            <Progress
              value={job.telemetry.progress * 100}
              aria-label={`Progress ${formatProgress(job.telemetry.progress)}`}
            />
            <span className="tabular-nums">{formatProgress(job.telemetry.progress)}</span>
          </div>
          <p className="text-muted-foreground text-sm tabular-nums">
            {formatSpeed(job.telemetry.bytesPerSecond)} ·{" "}
            {formatEta(job.telemetry.etaSeconds ?? null)}
            {job.telemetry.peers != null ? ` · ${job.telemetry.peers} peers` : ""}
          </p>
          {job.telemetry.stalled ? (
            <p className="text-state-warning text-sm">
              No progress for a while. The provider may be struggling to find sources.
            </p>
          ) : null}
        </div>
      ) : null}

      {job.state === "failed" || job.state === "gone" ? (
        <div className="space-y-1">
          <p className="text-state-failed">{failure.text}</p>
          <p className="text-muted-foreground text-sm">{failure.action}</p>
        </div>
      ) : null}

      <dl className="space-y-2">
        <Row label="Kind">{KIND_LABELS[job.kind] ?? job.kind}</Row>
        <Row label="Job ID">
          <span className="inline-flex items-center gap-2">
            <code className="text-xs font-mono">{job.id}</code>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Copy job ID"
              onClick={() => void copyJobId()}
            >
              <Copy className="size-3" aria-hidden="true" />
            </Button>
          </span>
        </Row>
        <Row label="Delivery">
          Single files: {job.delivery.perFile ? "yes" : "no"} · Archive:{" "}
          {bundle ? bundle.format.toUpperCase() : "—"} · Resumable archive:{" "}
          {bundle ? (bundle.rangeSupported ? "yes" : "no") : "—"}
        </Row>
      </dl>
    </div>
  );
}
