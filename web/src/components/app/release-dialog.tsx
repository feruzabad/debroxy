import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { removeJob } from "@/api/events";
import { keys } from "@/api/keys";
import { releaseJob } from "@/api/mutations/jobs";
import type { Job, JobList } from "@/api/queries/jobs";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { stateInfo } from "@/lib/states";

/** The release confirmation dialog (§5.6.4), shared by the jobs and detail pages. */
export function ReleaseDialog({
  job,
  providerName,
  open,
  onOpenChange,
  onReleased,
}: {
  job: Job;
  providerName?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onReleased?: () => void;
}) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const provider = providerName ?? job.provider;

  const doRelease = async () => {
    setBusy(true);
    try {
      await releaseJob(job.id);
      toast.success(`Released ${job.displayName}.`);
    } catch (error) {
      if (!isApiError(error) || error.slug !== "job-not-found") {
        toast.error(describeError(error).title, { duration: 10_000 });
        setBusy(false);
        return;
      }
      toast.success("Already released.");
    }
    queryClient.setQueryData<JobList>(keys.jobs, (list) =>
      list === undefined ? list : { ...list, jobs: removeJob(list.jobs, job.id) },
    );
    queryClient.removeQueries({ queryKey: keys.job(job.id) });
    queryClient.removeQueries({ queryKey: ["links", job.id] });
    void queryClient.invalidateQueries({ queryKey: keys.me });
    void queryClient.invalidateQueries({ queryKey: keys.history });
    setBusy(false);
    onOpenChange(false);
    onReleased?.();
  };

  const isActive = stateInfo(job.state).group === "active";

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Release {job.displayName}?</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-1">
              <p>
                {job.shared
                  ? "Others also use this content. It stays at the provider for them; it just leaves your list."
                  : `Nobody else uses this content, so debroxy will delete it at ${provider}. Download links stop working.`}
              </p>
              {isActive ? <p>It isn&apos;t finished yet.</p> : null}
              {job.slotCharged ? <p>This frees one of your slots.</p> : null}
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={busy} onClick={() => void doRelease()}>
            Release
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
