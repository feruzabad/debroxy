import { AlertTriangle } from "lucide-react";
import { describeError } from "@/api/errors";
import { Button } from "@/components/ui/button";

/** The inline error panel of §7.5, with "Try again". */
export function ErrorPanel({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const { title, text } = describeError(error);
  return (
    <div role="alert" className="flex flex-col items-start gap-2 rounded-md border p-4">
      <p className="flex items-center gap-2 font-medium">
        <AlertTriangle className="text-state-failed size-4" aria-hidden="true" />
        {title}
      </p>
      {text !== "" ? <p className="text-muted-foreground text-sm">{text}</p> : null}
      <Button variant="outline" size="sm" onClick={onRetry}>
        Try again
      </Button>
    </div>
  );
}
