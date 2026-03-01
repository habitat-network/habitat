import { Agent } from "@atproto/api";
import type {
  ComAtprotoRepoCreateRecord,
  ComAtprotoRepoGetRecord,
  ComAtprotoRepoListRecords,
} from "@atproto/api";
import type { DidDocument, DidResolver } from "@atproto/identity";
import type {
  NetworkHabitatRepoGetRecord,
  NetworkHabitatRepoListRecords,
  NetworkHabitatRepoPutRecord,
} from "api";
import { AuthManager } from "./authManager";

// Response types for HabitatClient - using generated types with generic overrides

// For putPrivateRecord: use generated OutputSchema
export type PutPrivateRecordResponse = NetworkHabitatRepoPutRecord.OutputSchema;

// For getPrivateRecord: override value field with generic
export type GetPrivateRecordResponse<T = Record<string, unknown>> = Omit<
  NetworkHabitatRepoGetRecord.OutputSchema,
  "value"
> & {
  value: T;
};

// For listPrivateRecords: override records array value field with generic
export type ListPrivateRecordsResponse<T = Record<string, unknown>> = Omit<
  NetworkHabitatRepoListRecords.OutputSchema,
  "records"
> & {
  records: Array<
    Omit<NetworkHabitatRepoListRecords.Record, "value"> & {
      value: T;
    }
  >;
};

// Legacy response types for public record operations (using atproto types)
export interface CreateRecordResponse {
  uri: string;
  cid: string;
}

export interface GetRecordResponse<T = Record<string, unknown>> {
  uri: string;
  cid?: string;
  value: T;
}

export interface ListRecordsResponse<T = Record<string, unknown>> {
  records: Array<{
    uri: string;
    cid: string;
    value: T;
  }>;
  cursor?: string;
}

// Input types for Habitat private record operations - using generated types with generic overrides
export type PutPrivateRecordInput<T = Record<string, unknown>> = Omit<
  NetworkHabitatRepoPutRecord.InputSchema,
  "record"
> & {
  record: T;
};

export type GetPrivateRecordParams = NetworkHabitatRepoGetRecord.QueryParams;
export type ListPrivateRecordsParams =
  NetworkHabitatRepoListRecords.QueryParams;

// HabitatAgentSession implements the Atproto Session interface.
export class HabitatAgentSession {
  serverUrl: string;

  constructor(serverUrl: string) {
    this.serverUrl = serverUrl;
  }

  async fetchHandler(pathname: string, init?: RequestInit): Promise<Response> {
    const url = new URL(pathname, this.serverUrl);
    const fetchReq = new Request(url.toString(), init);

    const response = await fetch(fetchReq);
    return response;
  }
}

export class HabitatAuthedAgentSession extends HabitatAgentSession {
  private authManager: AuthManager;

  constructor(serverUrl: string, authManager: AuthManager) {
    super(serverUrl);
    this.authManager = authManager;
  }

  async fetchHandler(pathname: string, init?: RequestInit): Promise<Response> {
    const url = `${this.serverUrl}${pathname}`;
    const method = init?.method ?? "GET";
    const body = init?.body as string | undefined;
    const headers = new Headers(init?.headers);

    const response = await this.authManager.fetch(url, method, body, headers);
    if (!response) {
      throw new Error(`Failed to fetch: ${url}`);
    }
    return response;
  }
}

export const getAgent = (serverUrl: string): Agent => {
  const session = new HabitatAgentSession(serverUrl);
  return new Agent(session);
};

export class HabitatClient {
  private defaultDid: string;
  private defaultAgent: Agent;
  private agents: Map<string, Agent>;
  private didResolver: DidResolver;

  constructor(did: string, defaultAgent: Agent, didResolver: DidResolver) {
    this.defaultDid = did;
    this.defaultAgent = defaultAgent;
    this.agents = new Map();
    this.agents.set(did, defaultAgent);
    this.didResolver = didResolver;
  }

  /**
   * Gets or creates an agent for the given DID.
   * If the agent doesn't exist, resolves the DID to find the PDS host and creates a new agent.
   * This should only be used for public records
   */
  private async getAgentForDid(did: string): Promise<Agent> {
    // Check if we already have an agent for this DID
    const existingAgent = this.agents.get(did);
    if (existingAgent) {
      return existingAgent;
    }

    // Resolve the DID to get the PDS host
    const didDoc: DidDocument | null = await this.didResolver.resolve(did);
    if (!didDoc) {
      throw new Error(`No DID document found for DID: ${did}`);
    }

    // Extract the PDS service endpoint
    const pdsService = didDoc.service?.find(
      (service) =>
        service.id === "#atproto_pds" ||
        service.type === "AtprotoPersonalDataServer",
    );

    if (!pdsService || !pdsService.serviceEndpoint) {
      throw new Error(`No PDS service found for DID: ${did}`);
    }

    // Ensure serviceEndpoint is a string
    const serviceEndpoint =
      typeof pdsService.serviceEndpoint === "string"
        ? pdsService.serviceEndpoint
        : String(pdsService.serviceEndpoint);

    // Parse the host from the service endpoint URL
    const serviceUrl = new URL(serviceEndpoint);

    // Create a new agent for this PDS
    const newAgent = getAgent(serviceUrl.toString());
    this.agents.set(did, newAgent);

    return newAgent;
  }

  /**
   * Resets the client by clearing all agents except the default one.
   * Useful for logout scenarios.
   */
  reset(): void {
    this.agents.clear();
    this.agents.set(this.defaultDid, this.defaultAgent);
  }

  async createRecord<T = Record<string, unknown>>(
    collection: string,
    record: T,
    rkey?: string,
    opts?: ComAtprotoRepoCreateRecord.CallOptions,
  ): Promise<CreateRecordResponse> {
    // Creating records always happens on the user's own repo
    const response = await this.defaultAgent.com.atproto.repo.createRecord(
      {
        repo: this.defaultDid,
        collection,
        record: record as Record<string, unknown>,
        rkey,
      },
      opts,
    );

    return {
      uri: response.data.uri,
      cid: response.data.cid,
    };
  }

  async putRecord<T = Record<string, unknown>>(
    collection: string,
    record: T,
    rkey: string,
    opts?: ComAtprotoRepoCreateRecord.CallOptions,
  ): Promise<CreateRecordResponse> {
    // Putting records always happens on the user's own repo
    const response = await this.defaultAgent.com.atproto.repo.putRecord(
      {
        repo: this.defaultDid,
        collection,
        record: record as Record<string, unknown>,
        rkey,
      },
      opts,
    );

    return {
      uri: response.data.uri,
      cid: response.data.cid,
    };
  }

  async getRecord<T = Record<string, unknown>>(
    collection: string,
    rkey: string,
    cid?: string,
    repo?: string,
    opts?: ComAtprotoRepoGetRecord.CallOptions,
  ): Promise<GetRecordResponse<T>> {
    // Determine which repo to query (default to user's own repo)
    const targetRepo = repo ?? this.defaultDid;

    // Get the appropriate agent for this repo's PDS
    const agent = await this.getAgentForDid(targetRepo);

    const response = await agent.com.atproto.repo.getRecord(
      {
        repo: targetRepo,
        collection,
        rkey,
        cid,
      },
      opts,
    );

    return {
      uri: response.data.uri,
      cid: response.data.cid,
      value: response.data.value as T,
    };
  }

  async listRecords<T = Record<string, unknown>>(
    collection: string,
    limit?: number,
    cursor?: string,
    repo?: string,
    opts?: ComAtprotoRepoListRecords.CallOptions,
    all: boolean = false,
  ): Promise<ListRecordsResponse<T>> {
    // Determine which repo to query (default to user's own repo)
    const targetRepo = repo ?? this.defaultDid;

    // Get the appropriate agent for this repo's PDS
    const agent = await this.getAgentForDid(targetRepo);

    let allRecords: Array<{ uri: string; cid: string; value: T }> = [];
    let currentCursor = cursor;

    do {
      const response = await agent.com.atproto.repo.listRecords(
        {
          repo: targetRepo,
          collection,
          limit,
          cursor: currentCursor,
        },
        opts,
      );

      allRecords = allRecords.concat(
        response.data.records.map(
          (record: { uri: string; cid: string; value: unknown }) => ({
            uri: record.uri,
            cid: record.cid,
            value: record.value as T,
          }),
        ),
      );

      currentCursor = response.data.cursor;
    } while (all && currentCursor);

    return {
      records: allRecords,
      cursor: currentCursor,
    };
  }

  async putPrivateRecord<T = Record<string, unknown>>(
    collection: string,
    record: T,
    rkey: string,
    grantees?: string[],
    opts?: RequestInit,
  ): Promise<PutPrivateRecordResponse> {
    // Writing private records always happens on the user's own repo
    const requestBody: PutPrivateRecordInput<T> = {
      repo: this.defaultDid,
      collection,
      rkey,
      record,
      // Cast needed: lexicon defines grantees as string unions but codegen wraps with $Typed
      grantees: grantees as PutPrivateRecordInput<T>["grantees"],
    };

    const response = await this.defaultAgent.fetchHandler(
      "/xrpc/network.habitat.putRecord",
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(requestBody),
        ...opts,
      },
    );

    if (!response.ok) {
      throw new Error(
        `Failed to put private record: ${response.status} ${response.statusText}`,
      );
    }

    return response.json();
  }

  async getPrivateRecord<T = Record<string, unknown>>(
    collection: string,
    rkey: string,
    repo?: string,
    opts?: RequestInit,
  ): Promise<GetPrivateRecordResponse<T>> {
    // Determine which repo to query (default to user's own repo)
    const targetRepo = repo ?? this.defaultDid;

    // Get the appropriate agent for this repo's PDS
    const agent = this.defaultAgent;

    const queryParams = new URLSearchParams({
      repo: targetRepo,
      collection,
      rkey,
    });

    const response = await agent.fetchHandler(
      `/xrpc/network.habitat.getRecord?${queryParams}`,
      {
        method: "GET",
        ...opts,
      },
    );

    if (!response.ok) {
      throw new Error(
        `Failed to get private record: ${response.status} ${response.statusText}`,
      );
    }

    return response.json();
  }

  async listPrivateRecords<T = Record<string, unknown>>(
    collection: string,
    limit?: number,
    cursor?: string,
    subjects?: string[],
    opts?: RequestInit,
  ): Promise<ListPrivateRecordsResponse<T>> {
    // Get the appropriate agent for this repo's PDS
    const agent = this.defaultAgent;

    const queryParams = new URLSearchParams();
    queryParams.set("collection", collection);

    if (limit !== undefined) {
      queryParams.set("limit", limit.toString());
    }
    if (cursor) {
      queryParams.set("cursor", cursor);
    }

    if (subjects) {
      for (const subject of subjects) {
        queryParams.append("subjects", subject);
      }
    }

    const response = await agent.fetchHandler(
      `/xrpc/network.habitat.listRecords?${queryParams}`,
      {
        method: "GET",
        ...opts,
      },
    );

    if (!response.ok) {
      throw new Error(
        `Failed to list private records: ${response.status} ${response.statusText}`,
      );
    }

    return response.json();
  }
}
