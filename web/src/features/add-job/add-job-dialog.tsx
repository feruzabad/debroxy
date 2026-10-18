import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { v7 as uuidv7 } from "uuid";
import { locateInfo, magnetFingerprint, torrentInfoHash } from "@/api/btih";
import { describeError, isApiError } from "@/api/errors";
import type { components } from "@/api/generated/job";
import { keys } from "@/api/keys";
import { type SubmitJobRequest, type SubmitOutcome, submitJob } from "@/api/mutations/jobs";
import { configQueryOptions } from "@/api/queries/config";
import type { JobList } from "@/api/queries/jobs";
import { cachedQueryOptions, hostersQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import { meQueryOptions } from "@/api/queries/me";
import { useCountdown } from "@/components/app/use-countdown";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  type AddJobSearch,
  displayNameSchema,
  magnetSchema,
  webUrlSchema,
} from "@/features/add-job/schema";
import { isCapabilityAvailable } from "@/lib/capabilities";
import { formatCountdown, formatRelativeTime, formatSize } from "@/lib/format";
import { defaultJobsSearch } from "@/lib/search";
import { readLastProvider, storeLastProvider } from "@/lib/storage";
import { cn } from "@/lib/utils";

type Provider = components["schemas"]["Provider"];
type Source = components["schemas"]["Source"];
type SourceType = "magnet" | "torrent" | "web";

const SOURCE_CAPABILITY: Record<SourceType, string> = {
  magnet: "SubmitMagnet",
  torrent: "SubmitTorrentFile",
  web: "SubmitWebURL",
};

const SOURCE_TAB_LABEL: Record<SourceType, string> = {
  magnet: "Magnet link",
  torrent: ".torrent file",
  web: "Web link",
};

function providerCapability(provider: Provider, name: string) {
  return provider.capabilities.find((capability) => capability.name === name);
}

function sourceAvailable(provider: Provider, source: SourceType): boolean {
  const capability = providerCapability(provider, SOURCE_CAPABILITY[source]);
  return capability !== undefined && isCapabilityAvailable(capability);
}

function cachedCheckAvailable(provider: Provider | null): boolean {
  if (provider === null) {
    return false;
  }
  const capability = providerCapability(provider, "CachedCheck");
  return capability !== undefined && isCapabilityAvailable(capability);
}

function pickProvider(
  providers: Provider[],
  source: SourceType,
  preferred: string | null,
): Provider | null {
  const enabled = providers.filter((p) => p.enabled && sourceAvailable(p, source));
  if (preferred !== null) {
    const match = enabled.find((p) => p.id === preferred);
    if (match !== undefined) {
      return match;
    }
  }
  const stored = readLastProvider();
  if (stored !== null) {
    const match = enabled.find((p) => p.id === stored);
    if (match !== undefined) {
      return match;
    }
  }
  return enabled[0] ?? null;
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

function computeTorrentFingerprint(bytes: Uint8Array): Promise<string | null> {
  if (typeof Worker === "undefined") {
    return torrentInfoHash(bytes);
  }
  return new Promise((resolve) => {
    const worker = new Worker(new URL("../../api/btih.worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = (event: MessageEvent<{ fingerprint: string | null }>) => {
      resolve(event.data.fingerprint);
      worker.terminate();
    };
    worker.onerror = () => {
      resolve(null);
      worker.terminate();
    };
    worker.postMessage({ id: 1, data: bytes.buffer });
  });
}

function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(id);
  }, [value, delay]);
  return debounced;
}

interface SubmitAlert {
  title: string;
  text: string | null;
  retry?: boolean;
  openJobId?: string;
  openList?: boolean;
  showActiveJobs?: boolean;
}

interface AddDraft {
  source: SourceType;
  providerId: string;
  magnet: string;
  torrent: {
    fileName: string;
    sizeBytes: number;
    file: string;
    fingerprint: string | null;
  } | null;
  webUrl: string;
  webPassword: string;
  name: string;
}

// A form state saved when the 200 "released-before" outcome closes the dialog,
// so the toast's "Add again" action can reopen it prefilled (§5.5.5).
let pendingDraft: AddDraft | null = null;

/** The Add job dialog (§5.5), overlaid on the jobs page. */
export function AddJobDialog({ search }: { search: AddJobSearch }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const configQuery = useQuery(configQueryOptions);
  const providersQuery = useQuery(providersQueryOptions);
  const meQuery = useQuery(meQueryOptions);

  const config = configQuery.data;
  const providers: Provider[] = providersQuery.data?.providers ?? [];
  const me = meQuery.data;
  const uploadLimit = config?.uploadLimitBytes ?? 0;

  const enabledProviders = providers.filter((p) => p.enabled);
  const visibleSources: SourceType[] = (["magnet", "torrent", "web"] as const).filter((source) =>
    enabledProviders.some((p) => sourceAvailable(p, source)),
  );

  const [tab, setTab] = useState<SourceType | null>(null);
  const [providerId, setProviderId] = useState<string | null>(null);
  const [magnet, setMagnet] = useState("");
  const [torrent, setTorrent] = useState<{
    fileName: string;
    sizeBytes: number;
    file: string;
    fingerprint: string | null;
  } | null>(null);
  const [webUrl, setWebUrl] = useState("");
  const [webPassword, setWebPassword] = useState("");
  const [name, setName] = useState("");

  const [fieldError, setFieldError] = useState<string | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [alert, setAlert] = useState<SubmitAlert | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [rateLimitedUntil, setRateLimitedUntil] = useState<number | null>(null);
  const rateRemaining = useCountdown(rateLimitedUntil);
  const [retryAt, setRetryAt] = useState<number | null>(null);
  const retryRemaining = useCountdown(retryAt);

  const jobIdRef = useRef<string>(uuidv7());

  const initialized = useRef(false);
  useEffect(() => {
    if (initialized.current) {
      return;
    }
    if (pendingDraft !== null) {
      initialized.current = true;
      const draft = pendingDraft;
      pendingDraft = null;
      setTab(draft.source);
      setProviderId(draft.providerId);
      setMagnet(draft.magnet);
      setTorrent(draft.torrent);
      setWebUrl(draft.webUrl);
      setWebPassword(draft.webPassword);
      setName(draft.name);
      return;
    }
    if (visibleSources.length === 0) {
      return;
    }
    initialized.current = true;
    const desired =
      search.source !== undefined && visibleSources.includes(search.source)
        ? search.source
        : visibleSources[0];
    if (desired === undefined) {
      return;
    }
    const provider = pickProvider(providers, desired, search.provider ?? null);
    setTab(desired);
    setProviderId(provider?.id ?? null);
  }, [providers, search.source, search.provider, visibleSources]);

  const provider = providers.find((p) => p.id === providerId) ?? null;

  const hostersQuery = useQuery({
    ...hostersQueryOptions(providerId ?? ""),
    enabled: tab === "web" && providerId !== null,
  });

  const fingerprint =
    tab === "magnet"
      ? magnetFingerprint(magnet)
      : tab === "torrent"
        ? (torrent?.fingerprint ?? null)
        : null;
  const debouncedFingerprint = useDebouncedValue(fingerprint, 400);
  const cachedQuery = useQuery({
    ...cachedQueryOptions(providerId ?? "", debouncedFingerprint ?? ""),
    enabled: cachedCheckAvailable(provider) && tab !== "web" && debouncedFingerprint !== null,
  });
  const cachedHit = cachedQuery.data === true;
  const cachedMiss = cachedQuery.data === false;

  const rateClass = cachedHit ? me?.submitRate.cached : me?.submitRate.uncached;
  const left = rateClass === undefined ? 0 : Math.max(0, rateClass.perHour - rateClass.used);
  const rateExhausted = left === 0;
  const max = me?.quota.maxActiveJobs ?? 0;

  const close = () => void navigate({ to: "/", search: defaultJobsSearch, replace: true });

  const onTabChange = (next: string) => {
    const source = next as SourceType;
    setTab(source);
    setFieldError(null);
    setAlert(null);
    const current = providers.find((p) => p.id === providerId);
    if (current === undefined || !sourceAvailable(current, source)) {
      setProviderId(pickProvider(providers, source, readLastProvider())?.id ?? null);
    }
  };

  const onProviderChange = (next: string) => {
    setProviderId(next);
    setFieldError(null);
    setAlert(null);
  };

  const handleFile = async (file: File) => {
    setFileError(null);
    const nameOk =
      file.name.toLowerCase().endsWith(".torrent") || file.type === "application/x-bittorrent";
    if (!nameOk) {
      setFileError("This file isn't a valid .torrent.");
      return;
    }
    if (uploadLimit > 0 && file.size > uploadLimit) {
      setFileError(
        `This file is ${formatSize(file.size)}; the limit is ${formatSize(uploadLimit)}.`,
      );
      return;
    }
    const buffer = await file.arrayBuffer();
    const bytes = new Uint8Array(buffer);
    if (locateInfo(bytes) === null) {
      setFileError("This file isn't a valid .torrent.");
      return;
    }
    const fileFingerprint = await computeTorrentFingerprint(bytes);
    setTorrent({
      fileName: file.name,
      sizeBytes: file.size,
      file: arrayBufferToBase64(buffer),
      fingerprint: fileFingerprint,
    });
  };

  const buildSource = (): Source | null => {
    if (tab === "magnet") {
      return { type: "magnet", magnet };
    }
    if (tab === "torrent") {
      if (torrent === null) {
        return null;
      }
      return { type: "torrent-file", fileName: torrent.fileName, file: torrent.file };
    }
    if (webPassword !== "") {
      return { type: "web-url", url: webUrl, password: webPassword };
    }
    return { type: "web-url", url: webUrl };
  };

  const handleSuccess = (outcome: SubmitOutcome) => {
    if (outcome.kind === "released-before") {
      const draft: AddDraft = {
        source: tab ?? "magnet",
        providerId: providerId ?? "",
        magnet,
        torrent,
        webUrl,
        webPassword,
        name,
      };
      toast.info(
        `You added and released this before (${formatRelativeTime(outcome.entry.releasedAt)}). Add it again?`,
        {
          action: {
            label: "Add again",
            onClick: () => {
              pendingDraft = draft;
              void navigate({ to: "/add" });
            },
          },
        },
      );
      close();
      return;
    }
    const job = outcome.job;
    queryClient.setQueryData(keys.job(job.id), job);
    queryClient.setQueryData<JobList>(keys.jobs, (list) =>
      list === undefined
        ? list
        : { ...list, jobs: [job, ...list.jobs.filter((entry) => entry.id !== job.id)] },
    );
    void queryClient.invalidateQueries({ queryKey: keys.me });
    storeLastProvider(providerId ?? "");
    const action = {
      label: "Open",
      onClick: () =>
        void navigate({ to: "/jobs/$jobId", params: { jobId: job.id }, search: { tab: "files" } }),
    };
    const providerName = providers.find((p) => p.id === providerId)?.displayName ?? "";
    if (outcome.kind === "pending") {
      toast.success(
        `${providerName} is still accepting ${job.displayName}. It will appear as soon as it's confirmed.`,
        { action },
      );
    } else {
      toast.success(`Added ${job.displayName}.`, { action });
    }
    close();
  };

  const handleSubmitError = (error: unknown) => {
    if (!isApiError(error)) {
      setAlert({ title: "Something went wrong.", text: null });
      return;
    }
    const providerName = provider?.displayName ?? "";
    switch (error.slug) {
      case "network-error":
        setAlert({
          title: "Couldn't reach debroxy. Your job may still have been added.",
          text: null,
          retry: true,
        });
        return;
      case "job-id-conflict": {
        jobIdRef.current = uuidv7();
        setSubmitting(true);
        void submitOnce(jobIdRef.current)
          .then(handleSuccess)
          .catch((err: unknown) => {
            if (isApiError(err) && err.slug === "job-id-conflict") {
              const { title, text } = describeError(err);
              setAlert({ title, text });
            } else {
              handleSubmitError(err);
            }
          })
          .finally(() => setSubmitting(false));
        return;
      }
      case "invalid-source":
        setFieldError(
          describeError(error, {
            source: tab === "magnet" ? "magnet link" : tab === "torrent" ? "file" : "link",
          }).text,
        );
        return;
      case "upload-too-large":
        setFileError(`This file is larger than ${formatSize(uploadLimit)}.`);
        return;
      case "unsupported-hoster":
        setFieldError(`${providerName} doesn't support this site.`);
        return;
      case "invalid-display-name":
        setFieldError("Use at most 255 characters.");
        return;
      case "job-already-exists": {
        const jobId = error.location?.split("/").pop();
        if (jobId !== undefined && jobId !== "") {
          setAlert({ title: "Already added.", text: "You already have this.", openJobId: jobId });
        } else {
          setAlert({ title: "Already added.", text: "You already have this.", openList: true });
        }
        return;
      }
      case "slot-quota-exceeded":
        setAlert({
          title: "No free slots.",
          text: `All ${me?.quota.maxActiveJobs ?? 0} of your slots are in use. Release a job to add another.`,
          showActiveJobs: true,
        });
        return;
      case "submit-rate-exceeded":
        setRateLimitedUntil(Date.now() + (error.retryAfter ?? 0) * 1000);
        return;
      case "provider-not-found":
        void queryClient.invalidateQueries({ queryKey: keys.providers });
        setAlert({ title: "This provider no longer exists. Pick another.", text: null });
        return;
      case "remote-item-unavailable":
      case "remote-item-being-deleted": {
        const { title, text } = describeError(error, { provider: providerName });
        setAlert({ title, text, retry: true });
        return;
      }
      case "provider-unavailable": {
        const seconds = error.retryAfter;
        setAlert({
          title: `${providerName} isn't responding.`,
          text: seconds !== undefined ? `Try again in ${seconds}s.` : "Try again",
          retry: true,
        });
        setRetryAt(seconds !== undefined ? Date.now() + seconds * 1000 : null);
        return;
      }
      case "capability-not-supported":
      case "capability-not-enabled":
        void queryClient.invalidateQueries({ queryKey: keys.providers });
        setAlert({ title: describeError(error, { provider: providerName }).title, text: null });
        return;
      case "provider-disabled":
      case "capability-unavailable-for-plan":
      case "provider-account-limit": {
        void queryClient.invalidateQueries({ queryKey: keys.providers });
        const { title, text } = describeError(error, { provider: providerName });
        setAlert({ title, text });
        return;
      }
      default: {
        const { title, text } = describeError(error, { provider: providerName });
        setAlert({ title, text });
      }
    }
  };

  const submitOnce = async (jobId: string): Promise<SubmitOutcome> => {
    const source = buildSource();
    if (source === null) {
      setFieldError("This file isn't a valid .torrent.");
      throw new Error("no source");
    }
    const trimmedName = name.trim();
    const request: SubmitJobRequest = {
      id: jobId,
      providerId: providerId ?? "",
      ...(trimmedName !== "" ? { displayName: trimmedName } : {}),
      source,
    };
    return submitJob(request);
  };

  const submit = async () => {
    if (tab === "magnet") {
      const result = magnetSchema.safeParse(magnet);
      if (!result.success) {
        setFieldError(result.error.issues[0]?.message ?? "This doesn't look like a magnet link.");
        return;
      }
    } else if (tab === "web") {
      const result = webUrlSchema.safeParse(webUrl);
      if (!result.success) {
        setFieldError(result.error.issues[0]?.message ?? "Enter a full http(s) link.");
        return;
      }
    } else if (torrent === null) {
      setFieldError("This file isn't a valid .torrent.");
      return;
    }
    const nameResult = displayNameSchema.safeParse(name);
    if (!nameResult.success) {
      setFieldError(nameResult.error.issues[0]?.message ?? "Use at most 255 characters.");
      return;
    }
    setSubmitting(true);
    setAlert(null);
    setFieldError(null);
    setRetryAt(null);
    try {
      handleSuccess(await submitOnce(jobIdRef.current));
    } catch (error) {
      handleSubmitError(error);
    } finally {
      setSubmitting(false);
    }
  };

  const availableForCurrent = provider !== null && tab !== null && sourceAvailable(provider, tab);

  // §5.5.3: a non-blocking warning when the web host isn't on the provider's
  // up-hoster list; the server decides with `unsupported-hoster`.
  const hostWarning = (() => {
    if (tab !== "web") {
      return null;
    }
    let host: string;
    try {
      host = new URL(webUrl).hostname;
    } catch {
      return null;
    }
    const hosters = hostersQuery.data?.hosters ?? [];
    if (hosters.length === 0) {
      return null;
    }
    const matches = hosters.some(
      (hoster) =>
        hoster.up &&
        hoster.domains.some((domain) => host === domain || host.endsWith(`.${domain}`)),
    );
    return matches
      ? null
      : `${host} isn't on ${provider?.displayName ?? ""}'s list of supported sites.`;
  })();

  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Add job</DialogTitle>
          <DialogDescription>Add a magnet link, a .torrent file or a web link.</DialogDescription>
        </DialogHeader>

        <div className="grid gap-2">
          <Label htmlFor="add-provider">Provider</Label>
          <Select value={providerId ?? ""} onValueChange={onProviderChange}>
            <SelectTrigger id="add-provider">
              <SelectValue placeholder="Pick a provider" />
            </SelectTrigger>
            <SelectContent>
              {enabledProviders.map((entry) => {
                const supportsAny = visibleSources.some((source) => sourceAvailable(entry, source));
                return (
                  <SelectItem key={entry.id} value={entry.id} disabled={!supportsAny}>
                    {entry.displayName}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>

        {tab !== null ? (
          <Tabs value={tab} onValueChange={onTabChange}>
            <TabsList className="w-full">
              {(["magnet", "torrent", "web"] as const).map((source) => {
                if (!visibleSources.includes(source)) {
                  return null;
                }
                const enabledHere = provider !== null && sourceAvailable(provider, source);
                return (
                  <Tooltip key={source}>
                    <TooltipTrigger asChild>
                      <TabsTrigger value={source} disabled={!enabledHere}>
                        {SOURCE_TAB_LABEL[source]}
                      </TabsTrigger>
                    </TooltipTrigger>
                    {!enabledHere && provider !== null ? (
                      <TooltipContent>Not available at {provider.displayName}</TooltipContent>
                    ) : null}
                  </Tooltip>
                );
              })}
            </TabsList>
          </Tabs>
        ) : null}

        {tab === "magnet" ? (
          <div className="grid gap-2">
            <Label htmlFor="add-magnet">Magnet link</Label>
            <Textarea
              id="add-magnet"
              rows={3}
              value={magnet}
              onChange={(event) => setMagnet(event.target.value)}
              aria-invalid={fieldError !== null}
              aria-describedby={fieldError !== null ? "add-field-error" : undefined}
              placeholder="magnet:?xt=urn:btih:…"
            />
          </div>
        ) : null}

        {tab === "torrent" ? (
          <div className="grid gap-2">
            <Label htmlFor="add-torrent">.torrent file</Label>
            <Input
              id="add-torrent"
              type="file"
              accept=".torrent,application/x-bittorrent"
              aria-invalid={fileError !== null}
              aria-describedby={fileError !== null ? "add-file-error" : undefined}
              onChange={(event) => {
                const file = event.target.files?.[0];
                if (file !== undefined) {
                  void handleFile(file);
                }
              }}
            />
            {torrent !== null ? (
              <div className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
                <span className="truncate">
                  {torrent.fileName} · {formatSize(torrent.sizeBytes)}
                </span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Remove file"
                  onClick={() => setTorrent(null)}
                >
                  ✕
                </Button>
              </div>
            ) : null}
          </div>
        ) : null}

        {tab === "web" ? (
          <div className="grid gap-2">
            <Label htmlFor="add-web">Web link</Label>
            <Input
              id="add-web"
              value={webUrl}
              onChange={(event) => setWebUrl(event.target.value)}
              aria-invalid={fieldError !== null}
              aria-describedby={fieldError !== null ? "add-field-error" : undefined}
              placeholder="https://example.com/file"
            />
            {hostWarning !== null ? (
              <p className="text-state-warning text-xs">{hostWarning}</p>
            ) : null}
            <Label htmlFor="add-web-password">Password (optional)</Label>
            <Input
              id="add-web-password"
              type="password"
              autoComplete="off"
              value={webPassword}
              onChange={(event) => setWebPassword(event.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              Only if the link is password-protected. If someone else already added this link, their
              password is used.
            </p>
            {hostersQuery.isError ? (
              <p className="text-state-failed text-xs">
                Can&apos;t load the list of supported sites.
              </p>
            ) : hostersQuery.data !== undefined ? (
              <Collapsible>
                <CollapsibleTrigger className="text-muted-foreground text-xs underline-offset-2 hover:underline">
                  Supported sites ({hostersQuery.data.hosters.length})
                </CollapsibleTrigger>
                <CollapsibleContent className="max-h-40 overflow-auto rounded-md border p-2 text-xs">
                  {hostersQuery.data.hosters.map((hoster) => (
                    <div key={hoster.name} className="flex items-center justify-between py-0.5">
                      <span>{hoster.name}</span>
                      <span className={cn(hoster.up ? "text-state-ready" : "text-state-failed")}>
                        {hoster.up ? "● up" : "○ down"}
                        {hoster.dailyLimit !== undefined && hoster.dailyUsed !== undefined
                          ? ` · ${hoster.dailyUsed} / ${hoster.dailyLimit} links today`
                          : ""}
                      </span>
                    </div>
                  ))}
                </CollapsibleContent>
              </Collapsible>
            ) : null}
          </div>
        ) : null}

        {tab !== null &&
        availableForCurrent &&
        cachedQuery.isPending &&
        debouncedFingerprint !== null ? (
          <p className="text-muted-foreground text-xs">Checking {provider?.displayName}…</p>
        ) : null}
        {tab !== null && availableForCurrent && cachedHit ? (
          <p className="text-state-ready text-xs">
            ⚡ Cached at {provider?.displayName}: ready almost instantly
          </p>
        ) : null}
        {tab !== null && availableForCurrent && cachedMiss ? (
          <p className="text-muted-foreground text-xs">
            Not cached at {provider?.displayName}: it will download first.
          </p>
        ) : null}

        <div className="grid gap-2">
          <Label htmlFor="add-name">Name (optional)</Label>
          <Input
            id="add-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            aria-invalid={fieldError !== null}
            aria-describedby={fieldError !== null ? "add-field-error" : undefined}
          />
        </div>

        {fieldError !== null ? (
          <p id="add-field-error" role="alert" className="text-state-failed text-xs">
            {fieldError}
          </p>
        ) : null}
        {fileError !== null ? (
          <p id="add-file-error" role="alert" className="text-state-failed text-xs">
            {fileError}
          </p>
        ) : null}
        {rateRemaining > 0 ? (
          <Alert variant="destructive">
            <AlertTitle>Submit limit reached.</AlertTitle>
            <AlertDescription>
              You&apos;ve reached {rateClass?.perHour ?? 0} submissions this hour. Try again in{" "}
              {formatCountdown(rateRemaining)}.
            </AlertDescription>
          </Alert>
        ) : null}
        {alert !== null ? (
          <Alert variant="destructive">
            <AlertTitle>{alert.title}</AlertTitle>
            {alert.text !== null ? <AlertDescription>{alert.text}</AlertDescription> : null}
            {alert.openJobId !== undefined ? (
              <AlertDescription>
                <Link
                  to="/jobs/$jobId"
                  params={{ jobId: alert.openJobId }}
                  search={{ tab: "files" }}
                  className="underline"
                >
                  Open it
                </Link>
              </AlertDescription>
            ) : alert.openList === true ? (
              <AlertDescription>
                <Link to="/" search={defaultJobsSearch} className="underline">
                  Open it
                </Link>
              </AlertDescription>
            ) : null}
            {alert.showActiveJobs === true ? (
              <AlertDescription>
                <Link
                  to="/"
                  search={{ ...defaultJobsSearch, group: "active" }}
                  className="underline"
                >
                  Show my active jobs
                </Link>
              </AlertDescription>
            ) : null}
            {alert.retry === true ? (
              <AlertDescription>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={retryRemaining > 0}
                  onClick={() => void submit()}
                >
                  Try again
                </Button>
              </AlertDescription>
            ) : null}
          </Alert>
        ) : null}

        <Tooltip>
          <TooltipTrigger asChild>
            <p className="text-muted-foreground text-xs">
              Uses 1 of your {max} slots ·{" "}
              {cachedHit
                ? `${left} of ${me?.submitRate.cached.perHour ?? 0} cached submits left`
                : `${left} of ${me?.submitRate.uncached.perHour ?? 0} submits left this hour`}
            </p>
          </TooltipTrigger>
          <TooltipContent>
            Joining content someone else already added doesn&apos;t use a slot or a submission.
          </TooltipContent>
        </Tooltip>

        <DialogFooter>
          <Button variant="outline" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={submitting || rateRemaining > 0 || rateExhausted || !availableForCurrent}
            onClick={() => void submit()}
          >
            {submitting
              ? "Adding…"
              : rateExhausted
                ? `Submit limit reached. Try again ${formatRelativeTime(rateClass?.resetsAt ?? Date.now())}.`
                : "Add job"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
