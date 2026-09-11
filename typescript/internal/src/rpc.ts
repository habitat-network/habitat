import {
  xrpc,
  type Agent,
  type AtIdentifierString,
  type NsidString,
} from "@atproto/lex";
import { network } from "api";
import { AuthManager } from "./authManager";

/**
 * Constructs an unauthenticated `Agent` that resolves request paths against an
 * explicit `https://${domain}` origin. Used to call public, auth-less XRPCs
 * (e.g. `network.habitat.instance.describeInstance`) on arbitrary instances.
 */
export const anonymousAgentFor = (domain: string): Agent => ({
  fetchHandler: async (path, init) => fetch(`https://${domain}${path}`, init),
});

export const castRecord = <T extends Record<string, unknown>>(record: {
  value: { [_ in string]: unknown };
}) => {
  return record.value as T;
};

export interface TypedRecord<T extends Record<string, unknown>> extends Omit<
  network.habitat.repo.getRecord.$OutputBody,
  "value"
> {
  value: T;
}

export const getPrivateRecord = async <
  T extends Record<string, unknown> = Record<string, unknown>,
>(
  authManager: AuthManager,
  collection: string,
  rkey: string,
  repo: string,
  includePermissions?: boolean,
): Promise<TypedRecord<T>> => {
  const response = await xrpc(
    authManager,
    network.habitat.repo.getRecord.main,
    {
      params: {
        collection: collection as NsidString,
        rkey,
        repo: repo as AtIdentifierString,
        includePermissions,
      },
    },
  );
  return response.body as unknown as TypedRecord<T>;
};

export interface ListRecordsResponse<
  T extends Record<string, unknown>,
> extends Omit<network.habitat.repo.listRecords.$OutputBody, "records"> {
  records: TypedRecord<T>[];
}

export const listPrivateRecords = async <T extends Record<string, unknown>>(
  authManager: AuthManager,
  collection: string,
  limit?: number,
  cursor?: string,
  subjects?: string[],
  includePermissions?: boolean,
): Promise<ListRecordsResponse<T>> => {
  const response = await xrpc(
    authManager,
    network.habitat.repo.listRecords.main,
    {
      params: {
        collection: collection as NsidString,
        limit,
        cursor,
        subjects: subjects ?? [],
        includePermissions,
      },
    },
  );
  return response.body as unknown as ListRecordsResponse<T>;
};
