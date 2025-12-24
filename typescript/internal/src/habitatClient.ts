import { Agent } from '@atproto/api';
import type { ComAtprotoRepoCreateRecord, ComAtprotoRepoGetRecord, ComAtprotoRepoListRecords } from '@atproto/api';
import { DidDocument, DidResolver } from '@atproto/identity';
import { AuthManager } from './authManager';

// Response types for HabitatClient
export interface CreateRecordResponse {
    uri: string;
    cid: string;
}

export interface GetRecordResponse<T = Record<string, unknown>> {
    uri: string;
    cid?: string;
    record: T;
}

export interface ListRecordsResponse<T = Record<string, unknown>> {
    records: Array<{
        uri: string;
        cid: string;
        value: T;
    }>;
    cursor?: string;
}

// Internal types for Habitat private record operations
// These include 'repo' since they're used in the wire protocol
interface PutRecordRequest<T = Record<string, unknown>> {
    collection: string;
    repo: string;
    rkey?: string;
    record: T;
}

// HabitatAgentSession implements the Atproto Session interface.
export class HabitatAgentSession {

    serverUrl: string;

    constructor(serverUrl: string) {
        this.serverUrl = serverUrl;
    }

    async fetchHandler(pathname: string, init?: RequestInit): Promise<Response> {
        const fetchReq = new Request(`${this.serverUrl}${pathname}`, init);

        const response = await fetch(fetchReq);
        return response;
    }
}

export class HabitatAuthedAgentSession extends HabitatAgentSession {
    private authManager: AuthManager;

    constructor(serverUrl: string, authManager: AuthManager ) {
        super(serverUrl);
        this.authManager = authManager;
    }

    async fetchHandler(pathname: string, init?: RequestInit): Promise<Response> {
        const fetchReq = new Request(`${this.serverUrl}${pathname}`, init);

        const response = await this.authManager.fetch(
            fetchReq.url,
            fetchReq.method,
            fetchReq.body,
            fetchReq.headers,
        );
        if (!response) {
            throw new Error(`Failed to fetch: ${fetchReq.url}`);
        }
        return response;
    }
}

export const getAgent = (serverUrl: string): Agent => {
    const session = new HabitatAgentSession(serverUrl);
    return new Agent(session);
}

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
            (service) => service.id === '#atproto_pds' || service.type === 'AtprotoPersonalDataServer'
        );

        if (!pdsService || !pdsService.serviceEndpoint) {
            throw new Error(`No PDS service found for DID: ${did}`);
        }

        // Ensure serviceEndpoint is a string
        const serviceEndpoint = typeof pdsService.serviceEndpoint === 'string' 
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
        const response = await this.defaultAgent.com.atproto.repo.createRecord({
            repo: this.defaultDid,
            collection,
            record: record as Record<string, unknown>,
            rkey,
        }, opts);
        
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
        const response = await this.defaultAgent.com.atproto.repo.putRecord({
            repo: this.defaultDid,
            collection,
            record: record as Record<string, unknown>,
            rkey,
        }, opts);
        
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
        
        const response = await agent.com.atproto.repo.getRecord({
            repo: targetRepo,
            collection,
            rkey,
            cid,
        }, opts);
        
        return {
            uri: response.data.uri,
            cid: response.data.cid,
            record: response.data.value as T,
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

        let allRecords: Array<{uri: string; cid: string; value: T}> = [];
        let currentCursor = cursor;

        do {
            const response = await agent.com.atproto.repo.listRecords({
                repo: targetRepo,
                collection,
                limit,
                cursor: currentCursor,
            }, opts);

            allRecords = allRecords.concat(
                response.data.records.map(record => ({
                    uri: record.uri,
                    cid: record.cid,
                    value: record.value as T,
                }))
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
        rkey?: string,
        opts?: RequestInit,
    ): Promise<CreateRecordResponse> {
        // Writing private records always happens on the user's own repo
        const requestBody: PutRecordRequest<T> = {
            repo: this.defaultDid,
            collection,
            rkey,
            record,
        };

        const response = await this.defaultAgent.fetchHandler('/xrpc/network.habitat.putRecord', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(requestBody),
            ...opts,
        });

        if (!response.ok) {
            throw new Error(`Failed to put private record: ${response.status} ${response.statusText}`);
        }

        return response.json();
    }

    async getPrivateRecord<T = Record<string, unknown>>(
        collection: string,
        rkey: string,
        cid?: string,
        repo?: string,
        opts?: RequestInit,
    ): Promise<GetRecordResponse<T>> {
        // Determine which repo to query (default to user's own repo)
        const targetRepo = repo ?? this.defaultDid;
        
        // Get the appropriate agent for this repo's PDS
        const agent = await this.getAgentForDid(targetRepo);
        
        const queryParams = new URLSearchParams({
            repo: targetRepo,
            collection,
            rkey,
        });

        if (cid) {
            queryParams.set('cid', cid);
        }

        const response = await agent.fetchHandler(`/xrpc/network.habitat.getRecord?${queryParams}`, {
            method: 'GET',
            ...opts,
        });

        if (!response.ok) {
            throw new Error(`Failed to get private record: ${response.status} ${response.statusText}`);
        }

        return response.json();
    }

    async listPrivateRecords<T = Record<string, unknown>>(
        collection: string,
        limit?: number,
        cursor?: string,
        repo?: string,
        opts?: RequestInit,
    ): Promise<ListRecordsResponse<T>> {
        // Determine which repo to query (default to user's own repo)
        const targetRepo = repo ?? this.defaultDid;
        
        // Get the appropriate agent for this repo's PDS
        const agent = await this.getAgentForDid(targetRepo);
        
        const queryParams = new URLSearchParams();
        queryParams.set('collection', collection);
        queryParams.set('repo', targetRepo);
        
        if (limit !== undefined) {
            queryParams.set('limit', limit.toString());
        }
        if (cursor) {
            queryParams.set('cursor', cursor);
        }

        const response = await agent.fetchHandler(`/xrpc/network.habitat.listRecords?${queryParams}`, {
            method: 'GET',
            ...opts,
        });

        if (!response.ok) {
            throw new Error(`Failed to list private records: ${response.status} ${response.statusText}`);
        }

        return response.json();
    }
}
