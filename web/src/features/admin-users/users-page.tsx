import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { adminUsersQueryOptions } from "@/api/queries/admin";
import { meQueryOptions } from "@/api/queries/me";
import { ErrorPanel } from "@/components/app/error-panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatExactTime } from "@/lib/format";
import { cn } from "@/lib/utils";

export interface UsersSearch {
  q?: string | undefined;
}

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

function mark(yes: boolean): string {
  return yes ? "✓" : "—";
}

/** The admin users list (§5.10), with a `?q` username filter. */
export function UsersPage({
  search,
  onSearchChange,
}: {
  search: UsersSearch;
  onSearchChange: (next: UsersSearch) => void;
}) {
  const navigate = useNavigate();
  const usersQuery = useQuery(adminUsersQueryOptions);
  const meQuery = useQuery(meQueryOptions);

  const users = usersQuery.data?.users ?? [];
  const needle = search.q?.trim().toLowerCase() ?? "";
  const filtered = users.filter(
    (user) => needle === "" || user.username.toLowerCase().includes(needle),
  );

  const setQuery = (q: string) => {
    const next = { ...search };
    if (q !== "") {
      next.q = q;
    } else {
      delete next.q;
    }
    onSearchChange(next);
  };

  if (usersQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (usersQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={usersQuery.error} onRetry={() => void usersQuery.refetch()} />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-3 px-6 pt-4 pb-2">
        <h1 className="text-base font-semibold">Users</h1>
        <div className="flex items-center gap-2">
          <Input
            value={search.q ?? ""}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="filter by username"
            aria-label="Filter users by username"
            className="h-7 max-w-xs"
          />
          <Button asChild size="sm">
            <Link to="/admin/users/new">+ New user</Link>
          </Button>
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Username</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Password</TableHead>
            <TableHead>SSO</TableHead>
            <TableHead>Created</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filtered.map((user) => (
            <TableRow
              key={user.id}
              tabIndex={0}
              className="cursor-pointer focus-visible:bg-muted/50 focus-visible:outline-none"
              onClick={() =>
                void navigate({ to: "/admin/users/$userId", params: { userId: user.id } })
              }
              onKeyDown={(event) => {
                if (event.key === "Enter" && event.target === event.currentTarget) {
                  void navigate({ to: "/admin/users/$userId", params: { userId: user.id } });
                }
              }}
            >
              <TableCell>
                <span className="font-medium">
                  {user.username}
                  {user.id === meQuery.data?.user.id ? " (you)" : ""}
                </span>
              </TableCell>
              <TableCell>{user.role === "admin" ? "Admin" : "User"}</TableCell>
              <TableCell>
                <span
                  className={cn(
                    user.status === "active" ? "text-state-ready" : "text-state-failed",
                  )}
                >
                  {user.status === "active" ? "● Active" : "○ Disabled"}
                </span>
              </TableCell>
              <TableCell>{mark(user.passwordLogin)}</TableCell>
              <TableCell>{mark(user.oidcLinked)}</TableCell>
              <TableCell className="text-muted-foreground tabular-nums">
                {formatExactTime(user.createdAt)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
