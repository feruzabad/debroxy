import type { Me } from "@/api/queries/me";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

const SLOTS_TOOLTIP =
  "Active jobs that count toward your limit, including ones still being added. Jobs shared with others count for whoever added them first, until they release them.";

function barColour(used: number, limit: number): string {
  if (limit > 0 && used >= limit) {
    return "bg-state-failed";
  }
  if (limit > 0 && used >= limit - 1) {
    return "bg-state-warning";
  }
  return "bg-primary";
}

function Meter({ used, limit }: { used: number; limit: number }) {
  const pct = limit > 0 ? Math.min(100, Math.max(0, (used / limit) * 100)) : 0;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-primary/20" aria-hidden="true">
      <div
        className={cn("h-full rounded-full", barColour(used, limit))}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

/** The sidebar quota meters (§5.2.3). */
export function QuotaMeters({ me }: { me: Me }) {
  const { quota, submitRate } = me;
  const max = quota.maxActiveJobs;
  const uncached = submitRate.uncached;
  const submitsAtLimit = uncached.perHour > 0 && uncached.used >= uncached.perHour;

  return (
    <div className="space-y-3">
      <Tooltip>
        <TooltipTrigger asChild>
          <div>
            <p className="text-xs text-muted-foreground">
              {max === 0 ? (
                <span className="text-state-failed font-medium">Slots: none allowed</span>
              ) : (
                <>
                  Slots <span className="tabular-nums">{quota.slotsUsed}</span> /{" "}
                  <span className="tabular-nums">{max}</span>
                </>
              )}
            </p>
            <Meter used={quota.slotsUsed} limit={max} />
          </div>
        </TooltipTrigger>
        <TooltipContent>{SLOTS_TOOLTIP}</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <div>
            <p
              className={cn("text-xs text-muted-foreground", submitsAtLimit && "text-state-failed")}
            >
              Submits <span className="tabular-nums">{uncached.used}</span> /{" "}
              <span className="tabular-nums">{uncached.perHour}</span> this hour
              {submitsAtLimit && uncached.resetsAt !== null ? (
                <> · ⟳ in {formatRelativeTime(uncached.resetsAt)}</>
              ) : null}
            </p>
            <Meter used={uncached.used} limit={uncached.perHour} />
          </div>
        </TooltipTrigger>
        <TooltipContent>
          Cached: {submitRate.cached.used} / {submitRate.cached.perHour}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}
