import { queryOptions } from "@tanstack/react-query";
import { wrappedFetch } from "@/api/client";
import { parseError } from "@/api/errors";
import { keys } from "@/api/keys";
import type { ClientConfig } from "@/api/types-rev13";

export const configQueryOptions = queryOptions({
  queryKey: keys.config,
  queryFn: async (): Promise<ClientConfig> => {
    const response = await wrappedFetch("/api/v1/config", {
      headers: { accept: "application/json" },
    });
    if (!response.ok) {
      throw await parseError(response);
    }
    return (await response.json()) as ClientConfig;
  },
  staleTime: Infinity,
});
