import { Clock } from "lucide-react";
import { useConnectionState } from "@/api/live";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

interface IndicatorState {
  label: string;
  tooltip: string;
  dot: string;
  pulse: boolean;
  clock: boolean;
}

const STATES: Record<ReturnType<typeof useConnectionState>, IndicatorState> = {
  live: {
    label: "Live",
    tooltip: "Live: updates appear as they happen.",
    dot: "bg-state-ready",
    pulse: false,
    clock: false,
  },
  connecting: {
    label: "Connecting…",
    tooltip: "Connecting…",
    dot: "bg-state-muted",
    pulse: true,
    clock: false,
  },
  reconnecting: {
    label: "Reconnecting…",
    tooltip: "Reconnecting… updates may be delayed.",
    dot: "bg-state-warning",
    pulse: false,
    clock: false,
  },
  polling: {
    label: "Offline",
    tooltip: "Live updates unavailable. Checking every 10 seconds.",
    dot: "bg-state-warning",
    pulse: false,
    clock: true,
  },
};

/** The header Event Stream indicator (§8.4). */
export function ConnectionIndicator() {
  const state = useConnectionState();
  const info = STATES[state];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span
            className={cn("size-2 rounded-full", info.dot, info.pulse && "animate-pulse")}
            aria-hidden="true"
          />
          {info.clock ? <Clock className="size-3.5" aria-hidden="true" /> : null}
          {info.label}
        </span>
      </TooltipTrigger>
      <TooltipContent>{info.tooltip}</TooltipContent>
    </Tooltip>
  );
}
