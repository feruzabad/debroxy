export type ApiSlug =
  | "invalid-request"
  | "invalid-source"
  | "upload-too-large"
  | "unsupported-hoster"
  | "provider-rejected"
  | "invalid-username"
  | "invalid-password"
  | "invalid-role"
  | "invalid-status"
  | "invalid-oidc-identity"
  | "wrong-current-password"
  | "no-login-method"
  | "invalid-display-name"
  | "invalid-slot-quota"
  | "webhook-too-large"
  | "invalid-credentials"
  | "authentication-required"
  | "session-expired"
  | "forbidden"
  | "invalid-signed-link"
  | "signed-link-expired"
  | "job-not-found"
  | "provider-not-found"
  | "user-not-found"
  | "not-found"
  | "job-already-exists"
  | "job-id-conflict"
  | "remote-item-being-deleted"
  | "remote-item-unavailable"
  | "remote-item-not-ready"
  | "target-not-available"
  | "last-admin"
  | "username-taken"
  | "oidc-identity-taken"
  | "provider-disabled"
  | "provider-account-limit"
  | "capability-unavailable-for-plan"
  | "bootstrap-not-allowed"
  | "slot-quota-exceeded"
  | "submit-rate-exceeded"
  | "refresh-rate-exceeded"
  | "login-throttled"
  | "capability-not-supported"
  | "capability-not-enabled"
  | "oidc-not-enabled"
  | "oidc-provider-unavailable"
  | "provider-unavailable"
  | "too-many-streams"
  | "internal-error"
  | "submission-pending"
  | "provider-state-stale"
  | "network-error"
  | `http-${number}`;

export class ApiError extends Error {
  readonly status: number;
  readonly slug: ApiSlug;
  readonly message: string;
  readonly retryAfter?: number;
  /** The `Location` header, set on `job-already-exists` (server C14-04). */
  readonly location?: string;

  constructor(init: {
    status: number;
    slug: ApiSlug;
    message: string;
    retryAfter?: number;
    location?: string;
  }) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.slug = init.slug;
    this.message = init.message;
    if (init.retryAfter !== undefined) {
      this.retryAfter = init.retryAfter;
    }
    if (init.location !== undefined) {
      this.location = init.location;
    }
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

interface ErrorBody {
  slug: string;
  message: string;
  retryAfter?: unknown;
}

function isErrorBody(value: unknown): value is ErrorBody {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return typeof record.slug === "string" && typeof record.message === "string";
}

function parseRetryAfterHeader(value: string | null): number | undefined {
  if (value === null) {
    return undefined;
  }
  if (/^\d+$/.test(value)) {
    return Number.parseInt(value, 10);
  }
  const retryDate = Date.parse(value);
  if (Number.isNaN(retryDate)) {
    return undefined;
  }
  return Math.max(1, Math.ceil((retryDate - Date.now()) / 1000));
}

// The §7.2 slug table as data. `behaviour` is the §7.1 kind the error is
// primarily shown with; multi-kind slugs use their primary kind.
export type ErrorBehaviour =
  | "inline"
  | "alert"
  | "toast"
  | "banner"
  | "countdown"
  | "redirect"
  | "page"
  | "none";

export interface ErrorSpec {
  title: string;
  text: string;
  behaviour: ErrorBehaviour;
}

export const ERROR_SPECS: Record<string, ErrorSpec> = {
  "invalid-request": {
    title: "Invalid request.",
    text: "Something in this request is invalid: {message}",
    behaviour: "alert",
  },
  "invalid-source": {
    title: "Can't read this.",
    text: "debroxy couldn't read this {source}.",
    behaviour: "inline",
  },
  "upload-too-large": {
    title: "File too large.",
    text: "This file is larger than {uploadLimitBytes}.",
    behaviour: "inline",
  },
  "unsupported-hoster": {
    title: "Site not supported.",
    text: "{provider} doesn't support this site.",
    behaviour: "inline",
  },
  "provider-rejected": {
    title: "Refused by {provider}.",
    text: "{provider} refused this: {message}",
    behaviour: "alert",
  },
  "invalid-username": {
    title: "Invalid username.",
    text: "Use 3–32 characters: letters, digits, dot, dash, underscore.",
    behaviour: "inline",
  },
  "invalid-password": {
    title: "Invalid password.",
    text: "Use at least {passwordMinLength} characters and at most 1024 bytes.",
    behaviour: "inline",
  },
  "invalid-role": {
    title: "Invalid role.",
    text: "Pick User or Admin.",
    behaviour: "inline",
  },
  "invalid-status": {
    title: "Invalid status.",
    text: "Pick Active or Disabled.",
    behaviour: "inline",
  },
  "invalid-oidc-identity": {
    title: "Invalid identity.",
    text: "The issuer must be an https URL and the subject must not be empty.",
    behaviour: "inline",
  },
  "wrong-current-password": {
    title: "Wrong password.",
    text: "That isn't your current password.",
    behaviour: "inline",
  },
  "no-login-method": {
    title: "No way to sign in.",
    text: "{username} would have no way to sign in. Set a password first.",
    behaviour: "alert",
  },
  "invalid-display-name": {
    title: "Name too long.",
    text: "Use at most 255 characters.",
    behaviour: "inline",
  },
  "invalid-slot-quota": {
    title: "Invalid quota.",
    text: "Enter a whole number, 0 or more.",
    behaviour: "inline",
  },
  "webhook-too-large": { title: "", text: "", behaviour: "none" },
  "invalid-credentials": {
    title: "Wrong username or password.",
    text: "",
    behaviour: "inline",
  },
  "authentication-required": { title: "", text: "", behaviour: "redirect" },
  "session-expired": { title: "", text: "", behaviour: "redirect" },
  forbidden: {
    title: "Not allowed.",
    text: "You don't have permission to do this.",
    behaviour: "toast",
  },
  "invalid-signed-link": {
    title: "Link invalid.",
    text: "This link is broken.",
    behaviour: "toast",
  },
  "signed-link-expired": { title: "Link expired.", text: "", behaviour: "toast" },
  "job-not-found": {
    title: "Job not found.",
    text: "This job doesn't exist or isn't yours.",
    behaviour: "page",
  },
  "provider-not-found": {
    title: "Provider not found.",
    text: "This provider no longer exists.",
    behaviour: "toast",
  },
  "user-not-found": { title: "User not found.", text: "", behaviour: "page" },
  "not-found": { title: "Not found.", text: "", behaviour: "toast" },
  "job-already-exists": {
    title: "Already added.",
    text: "You already have this.",
    behaviour: "alert",
  },
  "job-id-conflict": { title: "", text: "", behaviour: "none" },
  "remote-item-being-deleted": {
    title: "Being removed.",
    text: "This content is being deleted at {provider}. Try again in a minute.",
    behaviour: "alert",
  },
  "remote-item-unavailable": {
    title: "Changed meanwhile.",
    text: "This content changed while adding. Try again.",
    behaviour: "alert",
  },
  "remote-item-not-ready": {
    title: "Not ready yet.",
    text: "Downloads are available once the job is ready.",
    behaviour: "toast",
  },
  "target-not-available": {
    title: "Not available.",
    text: "This file or archive isn't available for download.",
    behaviour: "toast",
  },
  "last-admin": {
    title: "Last admin.",
    text: "{username} is the last active admin. Make someone else an admin first.",
    behaviour: "alert",
  },
  "username-taken": {
    title: "Username taken.",
    text: "Pick another username.",
    behaviour: "inline",
  },
  "oidc-identity-taken": {
    title: "Identity in use.",
    text: "Another user already has this identity.",
    behaviour: "inline",
  },
  "provider-disabled": {
    title: "Provider disabled.",
    text: "{provider} is turned off on this server.",
    behaviour: "alert",
  },
  "provider-account-limit": {
    title: "Provider account full.",
    text: "The {provider} account has reached its limit. Ask an admin.",
    behaviour: "alert",
  },
  "capability-unavailable-for-plan": {
    title: "Not on this plan.",
    text: "The {provider} account's plan doesn't include this.",
    behaviour: "alert",
  },
  "bootstrap-not-allowed": { title: "", text: "", behaviour: "none" },
  "slot-quota-exceeded": {
    title: "No free slots.",
    text: "All {max} of your slots are in use. Release a job to add another.",
    behaviour: "alert",
  },
  "submit-rate-exceeded": {
    title: "Submit limit reached.",
    text: "You've reached {perHour} submissions this hour. Try again in {m:ss}.",
    behaviour: "countdown",
  },
  "refresh-rate-exceeded": {
    title: "Slow down.",
    text: "You can refresh again in {s}s.",
    behaviour: "countdown",
  },
  "login-throttled": {
    title: "Too many attempts.",
    text: "Try again in {m:ss}.",
    behaviour: "countdown",
  },
  "capability-not-supported": {
    title: "Not supported.",
    text: "{provider} can't do this.",
    behaviour: "alert",
  },
  "capability-not-enabled": {
    title: "Not enabled.",
    text: "debroxy doesn't offer this yet.",
    behaviour: "alert",
  },
  "oidc-not-enabled": { title: "", text: "", behaviour: "none" },
  "oidc-provider-unavailable": { title: "", text: "", behaviour: "none" },
  "provider-unavailable": {
    title: "{provider} isn't responding.",
    text: "Try again{ in {s}s}.",
    behaviour: "alert",
  },
  "too-many-streams": {
    title: "Too many downloads.",
    text: "You have too many downloads running. Close one, or wait {s}s.",
    behaviour: "toast",
  },
  "internal-error": {
    title: "Something went wrong.",
    text: "debroxy hit an unexpected error. Try again; if it keeps happening, tell your admin.",
    behaviour: "toast",
  },
  "submission-pending": { title: "", text: "", behaviour: "none" },
  "provider-state-stale": { title: "", text: "", behaviour: "none" },
  "network-error": {
    title: "Can't reach debroxy.",
    text: "Check your connection and try again.",
    behaviour: "toast",
  },
};

/** The presentation spec for a slug, with the unknown-slug and `http-{status}` fallbacks. */
export function errorSpec(slug: string): ErrorSpec {
  if (slug.startsWith("http-")) {
    return {
      title: `Unexpected response (${slug.slice("http-".length)}).`,
      text: "",
      behaviour: "toast",
    };
  }
  return ERROR_SPECS[slug] ?? { title: `Error (${slug}).`, text: "{message}", behaviour: "toast" };
}

export async function parseError(response: Response): Promise<ApiError> {
  const status = response.status;
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = undefined;
  }
  if (isErrorBody(body)) {
    const bodyRetryAfter =
      typeof body.retryAfter === "number" && Number.isFinite(body.retryAfter)
        ? body.retryAfter
        : undefined;
    const retryAfter = bodyRetryAfter ?? parseRetryAfterHeader(response.headers.get("retry-after"));
    const location = response.headers.get("location") ?? undefined;
    return new ApiError({
      status,
      slug: body.slug as ApiSlug,
      message: body.message,
      ...(retryAfter !== undefined ? { retryAfter } : {}),
      ...(location !== undefined ? { location } : {}),
    });
  }
  const retryAfter = parseRetryAfterHeader(response.headers.get("retry-after"));
  return new ApiError({
    status,
    slug: `http-${status}`,
    message: "",
    ...(retryAfter !== undefined ? { retryAfter } : {}),
  });
}

/** Fills `{name}` placeholders of a §7.2 text. */
export function fillText(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in values ? String(values[name]) : match,
  );
}

/** The §7.2 title and text for an error, with `{message}` and the given values filled in. */
export function describeError(
  error: unknown,
  values: Record<string, string | number> = {},
): { title: string; text: string } {
  const apiError = isApiError(error)
    ? error
    : new ApiError({ status: 0, slug: "network-error", message: "" });
  const spec = errorSpec(apiError.slug);
  const filled = { message: apiError.message, ...values };
  return { title: fillText(spec.title, filled), text: fillText(spec.text, filled) };
}
