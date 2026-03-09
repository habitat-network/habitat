import type {
  ComAtprotoRepoCreateRecord,
  ComAtprotoRepoGetRecord,
  ComAtprotoRepoListRecords,
  ComAtprotoIdentityResolveHandle,
} from "@atproto/api";
import type {
  NetworkHabitatRepoGetRecord,
  NetworkHabitatRepoListCollections,
  NetworkHabitatRepoListRecords,
  NetworkHabitatRepoPutRecord,
} from "api";
import { AuthManager } from "./authManager";
import { DPoPOptions } from "openid-client";

type Query<
  Params extends Record<string, string | number | boolean | string[]>,
  Output,
> = {
  params: Params;
  output: Output;
};

type QueryEndpoints = {
  "com.atproto.repo.listRecords": Query<
    ComAtprotoRepoListRecords.QueryParams,
    ComAtprotoRepoListRecords.OutputSchema
  >;
  "com.atproto.repo.getRecord": Query<
    ComAtprotoRepoGetRecord.QueryParams,
    ComAtprotoRepoGetRecord.OutputSchema
  >;
  "network.habitat.getRecord": Query<
    NetworkHabitatRepoGetRecord.QueryParams,
    NetworkHabitatRepoGetRecord.OutputSchema
  >;
  "network.habitat.listRecords": Query<
    NetworkHabitatRepoListRecords.QueryParams,
    NetworkHabitatRepoListRecords.OutputSchema
  >;
  "com.atproto.identity.resolveHandle": Query<
    ComAtprotoIdentityResolveHandle.QueryParams,
    ComAtprotoIdentityResolveHandle.OutputSchema
  >;
  "network.habitat.repo.listCollections": Query<
    NetworkHabitatRepoListCollections.QueryParams,
    NetworkHabitatRepoListCollections.OutputSchema
  >;
};

type Procedure<Params, Output> = { params: Params; output: Output };

type ProcedureEndpoints = {
  "com.atproto.repo.createRecord": Procedure<
    ComAtprotoRepoCreateRecord.InputSchema,
    ComAtprotoRepoCreateRecord.OutputSchema
  >;
  "network.habitat.putRecord": Procedure<
    NetworkHabitatRepoPutRecord.InputSchema,
    NetworkHabitatRepoPutRecord.OutputSchema
  >;
};

interface QueryOptions {
  authManager: AuthManager;
  headers?: Headers;
  fetchOptions?: DPoPOptions;
}

export const query = async <T extends keyof QueryEndpoints>(
  endpoint: T,
  params: QueryEndpoints[T]["params"],
  options: QueryOptions,
): Promise<QueryEndpoints[T]["output"]> => {
  const queryParams = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const v of value) {
        queryParams.append(key, v.toString());
      }
    } else {
      queryParams.set(key, value.toString());
    }
  }
  const response = await options.authManager.fetch(
    "/xrpc/" + endpoint + "?" + queryParams.toString(),
    "GET",
    null,
    options.headers,
    options.fetchOptions,
  );
  try {
    const data = await response.json();
    if (!response.ok) {
      // TODO include data in thrown error
      console.error(data);
      throw new Error(
        `Failed to fetch: ${response.statusText}, ${response.status}`,
      );
    }
    return data;
  } catch {
    throw new Error(
      `Failed to fetch: ${response.statusText}, ${response.status}`,
    );
  }
};

export const procedure = async <T extends keyof ProcedureEndpoints>(
  endpoint: T,
  params: ProcedureEndpoints[T]["params"],
  options: QueryOptions,
): Promise<ProcedureEndpoints[T]["output"]> => {
  const response = await options.authManager.fetch(
    "/xrpc/" + endpoint,
    "POST",
    JSON.stringify(params),
    options.headers,
    options.fetchOptions,
  );
  try {
    const data = await response.json();
    if (!response.ok) {
      // TODO include data in thrown error
      console.error(data);
      throw new Error(
        `Failed to fetch: ${response.statusText}, ${response.status}`,
      );
    }
    return data;
  } catch {
    throw new Error(
      `Failed to fetch: ${response.statusText}, ${response.status}`,
    );
  }
};

export const castRecord = <T extends Record<string, unknown>>(record: {
  value: { [_ in string]: unknown };
}) => {
  return record.value as T;
};

export interface TypedRecord<T extends Record<string, unknown>>
  extends NetworkHabitatRepoGetRecord.OutputSchema {
  value: T;
}

export const getPrivateRecord = async <T = Record<string, unknown>>(
  authManager: AuthManager,
  collection: string,
  rkey: string,
  repo: string,
): Promise<NetworkHabitatRepoGetRecord.OutputSchema & { value: T }> => {
  const response = await query(
    "network.habitat.getRecord",
    { collection, rkey, repo },
    { authManager },
  );
  return response as NetworkHabitatRepoGetRecord.OutputSchema & { value: T };
};

interface ListRecordsResponse<T extends Record<string, unknown>>
  extends NetworkHabitatRepoListRecords.OutputSchema {
  records: TypedRecord<T>[];
}

export const listPrivateRecords = async <T extends Record<string, unknown>>(
  authManager: AuthManager,
  collection: string,
  limit?: number,
  cursor?: string,
  subjects?: string[],
): Promise<ListRecordsResponse<T>> => {
  const response = await query(
    "network.habitat.listRecords",
    { collection, limit, cursor, subjects: subjects ?? [] },
    { authManager },
  );
  return response as ListRecordsResponse<T>;
};

export const listCollections = async (
  authManager: AuthManager,
  subject: string,
): Promise<NetworkHabitatRepoListCollections.OutputSchema> => {
  return query(
    "network.habitat.repo.listCollections",
    { subject },
    { authManager },
  );
};
