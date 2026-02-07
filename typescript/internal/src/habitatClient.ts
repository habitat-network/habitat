import { Agent } from "@atproto/api";
import type {
  ComAtprotoRepoCreateRecord,
  ComAtprotoRepoGetRecord,
  ComAtprotoRepoListRecords,
} from "@atproto/api";
import type { DidDocument, DidResolver } from "@atproto/identity";
import type {
  NetworkHabitatNotificationCreateNotification,
  NetworkHabitatNotificationDefs,
  NetworkHabitatNotificationListNotifications,
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

// Re-export notification types from api for consumers
// Notification is the unified type used for both creating and listing notifications
export type Notification = NetworkHabitatNotificationDefs.Notification;
// CreateNotificationInput is used when creating notifications (same as Notification)
export type CreateNotificationInput = Notification;
// ListedNotification is the notification value returned from listNotifications (same as Notification)
export type ListedNotification = Notification;
// NotificationRecord is a record from listNotifications (includes uri, cid, value)
export type NotificationRecord =
  NetworkHabitatNotificationListNotifications.Record;
export type ListNotificationsResponse =
  NetworkHabitatNotificationListNotifications.OutputSchema;
export type CreateNotificationResponse =
  NetworkHabitatNotificationCreateNotification.OutputSchema;

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
    console.log("url", url.toString());
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
      console.log(`Using existing agent for DID: ${did}`);
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
        response.data.records.map((record) => ({
          uri: record.uri,
          cid: record.cid,
          value: record.value as T,
        })),
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
      grantees,
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
    repo?: string,
    opts?: RequestInit,
  ): Promise<ListPrivateRecordsResponse<T>> {
    // Determine which repo to query (default to user's own repo)
    const targetRepo = repo ?? this.defaultDid;

    // Get the appropriate agent for this repo's PDS
    const agent = this.defaultAgent;

    const queryParams = new URLSearchParams();
    queryParams.set("collection", collection);
    queryParams.set("repo", targetRepo);

    if (limit !== undefined) {
      queryParams.set("limit", limit.toString());
    }
    if (cursor) {
      queryParams.set("cursor", cursor);
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

  /**
   * Creates a notification targeting a specific DID.
   */
  async createNotification(
    notification: Notification,
    opts?: RequestInit,
  ): Promise<CreateNotificationResponse> {
    const response = await this.defaultAgent.fetchHandler(
      "/xrpc/network.habitat.notification.createNotification",
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          repo: this.defaultDid,
          collection: "network.habitat.notification",
          record: notification,
        }),
        ...opts,
      },
    );

    if (!response.ok) {
      throw new Error(
        `Failed to create notification: ${response.status} ${response.statusText}`,
      );
    }

    return response.json();
  }

  /**
   * Lists notifications for the authenticated user.
   */
  async listNotifications(
    collection?: string,
    opts?: RequestInit,
  ): Promise<ListNotificationsResponse> {
    const queryParams = new URLSearchParams();
    if (collection) {
      queryParams.set("collection", collection);
    }

    const url = queryParams.toString()
      ? `/xrpc/network.habitat.notification.listNotifications?${queryParams}`
      : "/xrpc/network.habitat.notification.listNotifications";

    const response = await this.defaultAgent.fetchHandler(url, {
      method: "GET",
      ...opts,
    });

    if (!response.ok) {
      throw new Error(
        `Failed to list notifications: ${response.status} ${response.statusText}`,
      );
    }

    return response.json();
  }
}
