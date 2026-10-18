import { queryOptions } from "@tanstack/react-query";
import { jobClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/job";
import { keys } from "@/api/keys";

export type Me = components["schemas"]["Me"];

export const meQueryOptions = queryOptions({
  queryKey: keys.me,
  queryFn: async () => {
    const { data } = await jobClient.GET("/api/v1/me");
    if (data === undefined) {
      throw new ApiError({ status: 0, slug: "network-error", message: "" });
    }
    return data;
  },
  staleTime: 30_000,
});
