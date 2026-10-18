import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, type LinkProps, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { ChevronDown, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { markJustLoggedOut } from "@/api/client";
import { isApiError } from "@/api/errors";
import { closeAllStreams, useOwnEventStream } from "@/api/live";
import { logOut } from "@/api/mutations/auth";
import { outboxMessagesQueryOptions } from "@/api/queries/admin";
import { configQueryOptions } from "@/api/queries/config";
import { jobsQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import { type Me, meQueryOptions } from "@/api/queries/me";
import type { ProviderWithStale } from "@/api/types-rev13";
import { ConnectionIndicator } from "@/components/app/connection-indicator";
import { QuotaMeters } from "@/components/app/quota-meters";
import { SessionBanner } from "@/components/app/session-banner";
import { ThemeToggle } from "@/components/app/theme-toggle";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { TooltipProvider } from "@/components/ui/tooltip";
import { defaultHistorySearch, defaultJobsSearch, parseJobsSearch } from "@/lib/search";
import { stateInfo } from "@/lib/states";

export function AppShell({ me: initialMe }: { me: Me }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  useOwnEventStream();

  const { data: config } = useQuery(configQueryOptions);
  const meQuery = useQuery(meQueryOptions);
  const jobsQuery = useQuery(jobsQueryOptions);
  const providersQuery = useQuery(providersQueryOptions);
  // §4.2: the sidebar "Stuck deletions" count comes from `['admin','outbox']`,
  // shown only once that query has been loaded (on the outbox page) this session.
  const outboxQuery = useQuery({ ...outboxMessagesQueryOptions, enabled: false });
  const outboxLoaded = outboxQuery.data !== undefined;
  const outboxCount = outboxQuery.data?.messages.length ?? 0;

  const me = meQuery.data ?? initialMe;
  const jobs = jobsQuery.data?.jobs ?? [];
  const providers: ProviderWithStale[] = providersQuery.data?.providers ?? [];
  const activeCount = jobs.filter((job) => stateInfo(job.state).group === "active").length;
  const providersWarning = providers.some(
    (provider) =>
      provider.stale === true ||
      provider.capabilities.some((capability) => capability.state === "unavailable"),
  );

  const [loggingOut, setLoggingOut] = useState(false);

  const handleLogout = async (redirectTo?: string) => {
    if (loggingOut) {
      return;
    }
    setLoggingOut(true);
    markJustLoggedOut();
    let serverError = false;
    try {
      await logOut();
    } catch (error) {
      if (isApiError(error) && (error.status === 401 || error.status === 0)) {
        // The session is already gone, or the server is unreachable.
      } else {
        serverError = true;
      }
    }
    if (serverError) {
      toast.error("Couldn't log out on the server. You've been logged out here.", {
        duration: 10_000,
      });
    }
    closeAllStreams();
    await queryClient.cancelQueries();
    queryClient.clear();
    await navigate({
      to: "/login",
      ...(redirectTo !== undefined ? { search: { redirect: redirectTo } } : {}),
      replace: true,
    });
  };

  const stayLoggedIn = () => {
    void queryClient.fetchQuery(meQueryOptions);
  };

  const logInAgain = () => {
    void handleLogout(`${window.location.pathname}${window.location.search}`);
  };

  return (
    <div className="bg-background text-foreground min-h-screen min-w-[1024px]">
      <TooltipProvider>
        <header className="bg-surface sticky top-0 z-20 flex h-12 items-center gap-4 border-b px-4">
          <Link
            to="/"
            search={defaultJobsSearch}
            className="text-base font-semibold"
            aria-label="debroxy home"
          >
            ◆ debroxy
          </Link>
          <HeaderSearch />
          <div className="ml-auto flex items-center gap-3">
            <ConnectionIndicator />
            <ThemeToggle />
            <UserMenu
              role={me.user.role}
              username={me.user.username}
              onLogout={() => void handleLogout()}
            />
          </div>
        </header>
        <SessionBanner me={me} onStayLoggedIn={stayLoggedIn} onLogInAgain={logInAgain} />
        <div className="flex">
          <nav
            aria-label="Main navigation"
            className="bg-surface sticky top-12 h-[calc(100vh-3rem)] w-56 shrink-0 overflow-y-auto border-r"
          >
            <div className="flex min-h-full flex-col gap-1 p-3">
              <Button asChild className="w-full">
                <Link to="/add">+ Add job</Link>
              </Button>
              <ul className="mt-2 flex flex-col gap-0.5">
                <NavItem exact to="/" search={defaultJobsSearch}>
                  Jobs{" "}
                  {activeCount > 0 ? (
                    <span className="text-muted-foreground">{activeCount}</span>
                  ) : null}
                </NavItem>
                <NavItem to="/history" search={defaultHistorySearch}>
                  History
                </NavItem>
                <NavItem to="/account">Account</NavItem>
              </ul>
              {me.user.role === "admin" ? (
                <section className="mt-3">
                  <h2 className="text-muted-foreground px-2 py-1 text-xs font-medium tracking-wide uppercase">
                    Admin
                  </h2>
                  <ul className="mt-0.5 flex flex-col gap-0.5">
                    <NavItem to="/admin/users">Users</NavItem>
                    <NavItem to="/admin/jobs" search={defaultJobsSearch}>
                      All jobs
                    </NavItem>
                    <NavItem to="/admin/providers">
                      Providers{" "}
                      {providersWarning ? (
                        <TriangleAlert
                          className="text-state-warning size-3"
                          aria-label="provider warning"
                        />
                      ) : null}
                    </NavItem>
                    <NavItem to="/admin/unmanaged">Unmanaged</NavItem>
                    <NavItem to="/admin/outbox">
                      Stuck deletions{" "}
                      {outboxLoaded && outboxCount > 0 ? (
                        <span className="text-muted-foreground">{outboxCount}</span>
                      ) : null}
                    </NavItem>
                  </ul>
                </section>
              ) : null}
              <div className="mt-auto space-y-2 pt-4">
                <QuotaMeters me={me} />
                {config ? <p className="text-muted-foreground text-xs">v{config.version}</p> : null}
              </div>
            </div>
          </nav>
          <main className="min-w-0 flex-1">
            <Outlet />
          </main>
        </div>
      </TooltipProvider>
    </div>
  );
}

function HeaderSearch() {
  const navigate = useNavigate();
  const location = useLocation();
  const pathname = location.pathname;
  const q = new URLSearchParams(location.searchStr).get("q") ?? "";
  const [draft, setDraft] = useState(q);

  useEffect(() => {
    setDraft(q);
  }, [q]);

  const onJobsPage = pathname === "/";

  const commit = (value: string) => {
    const search = onJobsPage ? parseJobsSearch(location.searchStr) : { ...defaultJobsSearch };
    if (value !== "") {
      search.q = value;
    } else {
      delete search.q;
    }
    void navigate({ to: "/", search, replace: onJobsPage });
  };

  return (
    <Input
      value={draft}
      onChange={(event) => {
        setDraft(event.target.value);
        if (onJobsPage) {
          commit(event.target.value);
        }
      }}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !onJobsPage) {
          commit(draft);
        }
      }}
      placeholder="Search jobs…"
      aria-label="Search jobs"
      className="h-7 max-w-xs"
    />
  );
}

function NavItem({
  to,
  exact = false,
  search,
  children,
}: {
  to: Exclude<LinkProps["to"], undefined>;
  exact?: boolean;
  search?: LinkProps["search"];
  children: React.ReactNode;
}) {
  return (
    <li>
      <Link
        to={to}
        {...(search !== undefined ? { search } : {})}
        activeOptions={{ exact }}
        className="hover:bg-accent flex h-7 items-center justify-between rounded-md px-2 text-sm"
        activeProps={{ className: "bg-accent font-medium" }}
      >
        {children}
      </Link>
    </li>
  );
}

function UserMenu({
  username,
  role,
  onLogout,
}: {
  username: string;
  role: "admin" | "user";
  onLogout: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="h-7 gap-1.5 px-2 text-sm"
          aria-label={`User menu: ${username}`}
        >
          <span>{username}</span>
          <Badge variant="outline">{role === "admin" ? "Admin" : "User"}</Badge>
          <ChevronDown className="size-3.5" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuItem asChild>
          <Link to="/account">Account</Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onLogout}>Log out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
