import {
  ArrowDownCircle,
  CheckCircle2,
  CircleHelp,
  CircleOff,
  Clock,
  Cog,
  ListChecks,
  LoaderCircle,
  type LucideIcon,
  Search,
  Trash,
  Trash2,
  TriangleAlert,
  XCircle,
} from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { type StateInfo, stateInfo } from "@/lib/states";
import { cn } from "@/lib/utils";

const ICONS: Record<string, LucideIcon> = {
  "loader-circle": LoaderCircle,
  clock: Clock,
  search: Search,
  "list-checks": ListChecks,
  "arrow-down-circle": ArrowDownCircle,
  cog: Cog,
  "check-circle-2": CheckCircle2,
  "x-circle": XCircle,
  "circle-off": CircleOff,
  "trash-2": Trash2,
  trash: Trash,
  "help-circle": CircleHelp,
};

/** Static class names per colour token, so Tailwind keeps them (§6.1, §9.1). */
export const COLOUR_CLASSES: Record<string, string> = {
  "--state-pending": "text-state-pending",
  "--state-active": "text-state-active",
  "--state-ready": "text-state-ready",
  "--state-warning": "text-state-warning",
  "--state-failed": "text-state-failed",
  "--state-muted": "text-state-muted",
};

interface JobLike {
  state: string;
  telemetry: { stalled: boolean };
  failureReason?: string | null;
}

export function jobStateInfo(job: JobLike, provider: string): StateInfo {
  return stateInfo(job.state, {
    stalled: job.telemetry.stalled,
    provider,
    failureReason: job.failureReason ?? null,
  });
}

export function StateIcon({ info }: { info: StateInfo }) {
  const Icon = info.stalled ? TriangleAlert : (ICONS[info.icon] ?? CircleHelp);
  const spin = info.state === "submitting" && !info.stalled;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn("inline-flex", COLOUR_CLASSES[info.colour])}>
          <Icon className={cn("size-4", spin && "animate-spin")} aria-label={info.label} />
        </span>
      </TooltipTrigger>
      {info.tooltip !== "" ? <TooltipContent>{info.tooltip}</TooltipContent> : null}
    </Tooltip>
  );
}

export function StateBadge({ info }: { info: StateInfo }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            "inline-flex h-5 items-center rounded-sm border px-1.5 text-xs font-medium whitespace-nowrap",
            COLOUR_CLASSES[info.colour],
          )}
        >
          {info.label}
        </span>
      </TooltipTrigger>
      {info.tooltip !== "" ? <TooltipContent>{info.tooltip}</TooltipContent> : null}
    </Tooltip>
  );
}
