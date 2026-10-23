import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export type LlmConnection = network.habitat.llm.defs.Connection;

export const llmConnectionsQueryKey = (org: DidString) =>
  ["llm", "connections", org] as const;

// orgLlmConnectionsQueryOptions lists the LLM model connections configured
// for org. API keys are never returned, only whether one is stored. Requires
// service-auth, so it's proxied to the org's own habitat instance via
// pearAgent.
export function orgLlmConnectionsQueryOptions(
  org: DidString,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: llmConnectionsQueryKey(org),
    queryFn: async (): Promise<LlmConnection[]> => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.llm.listConnections.main,
        { params: { org } },
      );
      return response.body.connections;
    },
  });
}

// The mutations below refetch the list before resolving, so isPending covers
// the refresh and callers can close their dialogs from onSuccess. All of them
// require the caller to hold the community.configure action.

// addLlmConnectionMutationOptions adds a model connection to org. The API key
// is stored encrypted by pear; the rest is written as a record.
export const addLlmConnectionMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      input: Omit<network.habitat.llm.addConnection.$InputBody, "org">,
      { client },
    ) => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.llm.addConnection.main,
        { body: { org, ...input } },
      );
      await client.invalidateQueries({ queryKey: llmConnectionsQueryKey(org) });
      return response.body.connection;
    },
  });

// updateLlmConnectionMutationOptions changes a connection's description,
// model, base URL or API key. Fields left out are unchanged.
export const updateLlmConnectionMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      input: Omit<network.habitat.llm.updateConnection.$InputBody, "org">,
      { client },
    ) => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.llm.updateConnection.main,
        { body: { org, ...input } },
      );
      await client.invalidateQueries({ queryKey: llmConnectionsQueryKey(org) });
      return response.body.connection;
    },
  });

// removeLlmConnectionMutationOptions deletes the connection identified by the
// mutation's id variable, along with its stored API key.
export const removeLlmConnectionMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (id: string, { client }) => {
      await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.llm.removeConnection.main,
        { body: { org, id } },
      );
      await client.invalidateQueries({ queryKey: llmConnectionsQueryKey(org) });
    },
  });
