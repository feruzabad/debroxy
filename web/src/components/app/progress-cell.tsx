import type { components } from "@/api/generated/job";
import { Progress } from "@/components/ui/progress";
import { formatEta, formatProgress, formatSpeed } from "@/lib/format";
import { failureText, stateInfo } from "@/lib/states";

type Job = components["schemas"]["Job"];

/** The Progress column of §5.3.3. */
export function ProgressCell({ job, provider }: { job: Job; provider: string }) {
  const group = stateInfo(job.state).group;
  if (job.state === "ready") {
    return <span className="text-muted-foreground">—</span>;
  }
  if (job.state === "failed" || job.state === "gone") {
    const text = failureText(job.failureReason ?? null, provider).text;
    return (
      <span className="text-state-failed block max-w-56 truncate" title={text}>
        {text}
      </span>
    );
  }
  if (group !== "active") {
    return <span className="text-muted-foreground">—</span>;
  }
  const { telemetry } = job;
  const percent = formatProgress(telemetry.progress);
  return (
    <div className="flex min-w-40 flex-col gap-0.5">
      <div className="flex items-center gap-2">
        <Progress
          value={telemetry.progress * 100}
          className="h-1.5 w-24"
          aria-label={`Progress ${percent}`}
        />
        <span className="tabular-nums">{percent}</span>
      </div>
      <span className="text-muted-foreground text-xs tabular-nums">
        {formatSpeed(telemetry.bytesPerSecond)} · {formatEta(telemetry.etaSeconds ?? null)}
        {telemetry.peers != null ? ` · ${telemetry.peers} peers` : ""}
      </span>
    </div>
  );
}
