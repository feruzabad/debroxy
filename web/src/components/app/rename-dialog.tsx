import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { describeError, isApiError } from "@/api/errors";
import { upsertJob } from "@/api/events";
import { keys } from "@/api/keys";
import { renameJob } from "@/api/mutations/jobs";
import type { Job, JobList } from "@/api/queries/jobs";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const renameSchema = z
  .string()
  .trim()
  .min(1, "Enter a name.")
  .max(255, "Use at most 255 characters.");

/** The rename dialog (§5.6.3), shared by the jobs page and the detail page. */
export function RenameDialog({
  job,
  open,
  onOpenChange,
}: {
  job: Job;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(job.displayName);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setName(job.displayName);
      setError(null);
    }
  }, [open, job.displayName]);

  const save = async () => {
    const result = renameSchema.safeParse(name);
    if (!result.success) {
      setError(result.error.issues[0]?.message ?? "Use at most 255 characters.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const updated = await renameJob(job.id, result.data);
      queryClient.setQueryData(keys.job(job.id), updated);
      queryClient.setQueryData<JobList>(keys.jobs, (list) =>
        list === undefined ? list : { ...list, jobs: upsertJob(list.jobs, updated) },
      );
      toast.success("Renamed.");
      onOpenChange(false);
    } catch (err) {
      if (isApiError(err) && err.slug === "job-not-found") {
        queryClient.setQueryData(keys.job(job.id), undefined);
        queryClient.removeQueries({ queryKey: keys.job(job.id) });
        toast.error(describeError(err).text, { duration: 10_000 });
        onOpenChange(false);
        return;
      }
      setError(describeError(err).title);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Rename</DialogTitle>
          <DialogDescription>Change the name shown in your job list.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="rename-name">Name</Label>
          <Input
            id="rename-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            aria-invalid={error !== null}
            aria-describedby={error !== null ? "rename-error" : undefined}
          />
          {error !== null ? (
            <p id="rename-error" className="text-state-failed text-xs">
              {error}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={saving}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
