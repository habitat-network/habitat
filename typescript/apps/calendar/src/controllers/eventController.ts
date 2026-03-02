import type {
  HabitatClient,
  PutPrivateRecordResponse,
} from "internal/habitatClient.ts";
import {
  CommunityLexiconCalendarEvent,
  CommunityLexiconCalendarRsvp,
  CommunityLexiconCalendarInvite,
} from "api";

// Re-export the lexicon types for convenience
export type CalendarEvent = CommunityLexiconCalendarEvent.Record;
export type Rsvp = CommunityLexiconCalendarRsvp.Record;
export type Invite = CommunityLexiconCalendarInvite.Record;

// StrongRef type used by RSVP.subject (matches com.atproto.repo.strongRef)
export interface StrongRef {
  uri: string;
  cid: string;
}

export interface RsvpWithEvent {
  uri: string;
  cid: string;
  rsvp: Rsvp;
  event: CalendarEvent | null;
}

export interface InviteWithEvent {
  uri: string;
  cid: string;
  invite: Invite;
  event: CalendarEvent | null;
}

export interface EventRecord {
  uri: string;
  cid: string;
  value: CalendarEvent;
}

/**
 * Builds a map from event URI to event data, combining owned events and invited events.
 * Filters out events that don't have required fields (name, startsAt).
 *
 * @param events - Event records owned by the user
 * @param invites - Invites the user has received
 * @param userDid - The current user's DID (to filter invites)
 * @returns Map from event URI to CalendarEvent
 */
export function buildEventDataMap(
  events: EventRecord[],
  invites: InviteWithEvent[],
  userDid?: string,
): Map<string, CalendarEvent> {
  const eventDataMap = new Map<string, CalendarEvent>();

  // Add displayable owned events
  for (const e of events) {
    if (e.value?.name && e.value?.startsAt) {
      eventDataMap.set(e.uri, e.value);
    }
  }

  // Build set of owned event URIs to avoid duplicates
  const ownedEventUris = new Set(events.map((e) => e.uri));

  // Add displayable invited events (that aren't already owned)
  for (const inv of invites) {
    if (!inv.event?.name || !inv.event?.startsAt) continue;
    if (userDid && inv.invite.invitee !== userDid) continue;
    const uri = inv.invite.subject?.uri || inv.uri;
    if (ownedEventUris.has(uri)) continue;
    eventDataMap.set(uri, inv.event);
  }

  return eventDataMap;
}

/**
 * Filters invites to only include displayable ones for the current user
 * that aren't already in the user's events list.
 */
export function getDisplayableInvites(
  invites: InviteWithEvent[],
  ownedEventUris: Set<string>,
  userDid?: string,
): InviteWithEvent[] {
  return invites.filter((inv) => {
    if (!inv.event?.name || !inv.event?.startsAt) return false;
    if (userDid && inv.invite.invitee !== userDid) return false;
    if (inv.invite.subject?.uri && ownedEventUris.has(inv.invite.subject.uri))
      return false;
    return true;
  });
}

const EVENT_COLLECTION = "community.lexicon.calendar.event";
const RSVP_COLLECTION = "community.lexicon.calendar.rsvp";
const INVITE_COLLECTION = "community.lexicon.calendar.invite";
const CLIQUE_COLLECTION = "network.habitat.clique";

function buildCliqueUri(ownerDid: string, rkey: string): string {
  return `habitat://${ownerDid}/${CLIQUE_COLLECTION}/${rkey}`;
}

/**
 * Parses a record URI to extract the DID, collection, and rkey.
 * Supports both at:// and habitat:// schemes.
 * Format: at://did:plc:xxx/collection.name/rkey or habitat://did:plc:xxx/collection.name/rkey
 */
function parseRecordUri(
  uri: string,
): { did: string; collection: string; rkey: string } | null {
  const match = uri.match(/^(?:at|habitat):\/\/([^/]+)\/([^/]+)\/([^/]+)$/);
  if (!match) return null;
  return { did: match[1], collection: match[2], rkey: match[3] };
}

/**
 * Creates a new calendar event with a clique for permission management.
 *
 * This creates:
 * 1. A clique that includes the creator and all invitees
 * 2. The event record, permissioned via the clique
 * 3. Invite records for each invitee, permissioned via the clique
 *
 * @param client - The Habitat client instance
 * @param userDid - The DID of the current user (event creator)
 * @param event - The event data (without createdAt, which is auto-generated)
 * @param invitedDids - List of DIDs for people to invite to this event
 * @returns The created event record response
 */
export async function createEvent(
  client: HabitatClient,
  userDid: string,
  event: Omit<CalendarEvent, "createdAt">,
  invitedDids: string[] = [],
): Promise<PutPrivateRecordResponse> {
  const eventRkey = crypto.randomUUID();
  const cliqueRkey = `event-${eventRkey}`;
  const cliqueUri = buildCliqueUri(userDid, cliqueRkey);
  const createdAt = new Date().toISOString();

  // Step 1: Create a clique with the creator and all invitees as members
  // The clique itself grants access to anyone in the list
  await client.putPrivateRecord(
    CLIQUE_COLLECTION,
    {}, // Clique record is empty, membership is defined by grantees
    cliqueRkey,
    { dids: [userDid, ...invitedDids] },
  );

  // Step 2: Create the event, granting access via the clique
  const eventRecord = {
    ...event,
    createdAt,
  } as CalendarEvent;

  const eventResponse = await client.putPrivateRecord<CalendarEvent>(
    EVENT_COLLECTION,
    eventRecord,
    eventRkey,
    { cliques: [cliqueUri] },
  );

  // Step 3: Create invite records for each invitee
  // These are also permissioned via the clique so all participants can see them
  const invitePromises = invitedDids.map((inviteeDid) => {
    const inviteRkey = crypto.randomUUID();
    const inviteRecord: Omit<Invite, "$type"> = {
      subject: {
        uri: eventResponse.uri,
        cid: "",
      },
      invitee: inviteeDid,
      createdAt,
    };
    return client.putPrivateRecord(
      INVITE_COLLECTION,
      inviteRecord,
      inviteRkey,
      { cliques: [cliqueUri] },
    );
  });

  await Promise.all(invitePromises);

  // Step 4: Auto-RSVP the creator as "going"
  const parsed = parseRecordUri(eventResponse.uri);
  if (parsed) {
    const creatorRsvpRkey = `rsvp-for-${parsed.did}-${parsed.rkey}`;
    const creatorRsvpRecord: Omit<Rsvp, "$type"> = {
      subject: {
        uri: eventResponse.uri,
        cid: "",
      },
      status: "community.lexicon.calendar.rsvp#going",
    };
    await client.putPrivateRecord(
      RSVP_COLLECTION,
      creatorRsvpRecord,
      creatorRsvpRkey,
      { cliques: [cliqueUri] },
    );
  }

  return eventResponse;
}

/**
 * Lists all events the user has access to (from their own repo and shared via notifications).
 *
 * @param client - The Habitat client instance
 */
export async function listEvents(client: HabitatClient) {
  return client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION);
}

/**
 * Lists all invites the user can see, with their corresponding event info.
 * Fetches all events and invites upfront, then matches them in memory.
 *
 * @param client - The Habitat client instance
 */
export async function listInvites(
  client: HabitatClient,
): Promise<InviteWithEvent[]> {
  const [invitesResponse, eventsResponse] = await Promise.all([
    client.listPrivateRecords<Invite>(INVITE_COLLECTION),
    client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION),
  ]);

  // Build event lookup map: "did/collection/rkey" -> event
  const eventMap = new Map<string, CalendarEvent>();
  for (const record of eventsResponse.records) {
    const parsed = parseRecordUri(record.uri);
    if (parsed) {
      const key = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
      eventMap.set(key, record.value);
    }
  }

  // Match invites to events
  const invitesWithEvents: InviteWithEvent[] = [];
  for (const record of invitesResponse.records) {
    if (!record.value?.subject?.uri) {
      console.error(
        "Invalid invite format, missing subject.uri:",
        record.uri,
        record.value,
      );
      continue;
    }

    const parsed = parseRecordUri(record.value.subject.uri);
    if (!parsed) {
      console.error(
        "Invalid subject URI in invite:",
        record.uri,
        record.value.subject.uri,
      );
      continue;
    }

    const eventKey = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
    const event = eventMap.get(eventKey) || null;

    invitesWithEvents.push({
      uri: record.uri,
      cid: record.cid,
      invite: record.value,
      event,
    });
  }

  return invitesWithEvents;
}

/**
 * Lists all RSVPs with their corresponding event info.
 * Fetches all events and RSVPs upfront, then matches them in memory.
 * Records with invalid format are logged and discarded.
 *
 * @param client - The Habitat client instance
 */
export async function listRsvps(
  client: HabitatClient,
): Promise<RsvpWithEvent[]> {
  // Fetch everything we need in parallel - "fetch the world"
  const [rsvpsResponse, eventsResponse] = await Promise.all([
    client.listPrivateRecords<Rsvp>(RSVP_COLLECTION),
    client.listPrivateRecords<CalendarEvent>(EVENT_COLLECTION),
  ]);

  // Build event lookup map: "did/collection/rkey" -> event
  const eventMap = new Map<string, CalendarEvent>();
  for (const record of eventsResponse.records) {
    const parsed = parseRecordUri(record.uri);
    if (parsed) {
      const key = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
      eventMap.set(key, record.value);
    }
  }

  // Match RSVPs to events
  const rsvpsWithEvents: RsvpWithEvent[] = [];
  for (const record of rsvpsResponse.records) {
    // Validate RSVP has expected schema
    if (!record.value?.subject?.uri) {
      console.error(
        "Invalid RSVP format, missing subject.uri:",
        record.uri,
        record.value,
      );
      continue;
    }

    const parsed = parseRecordUri(record.value.subject.uri);
    if (!parsed) {
      console.error(
        "Invalid subject URI in RSVP:",
        record.uri,
        record.value.subject.uri,
      );
      continue;
    }

    // Look up event in our pre-fetched map
    const eventKey = `${parsed.did}/${parsed.collection}/${parsed.rkey}`;
    const event = eventMap.get(eventKey) || null;

    rsvpsWithEvents.push({
      uri: record.uri,
      cid: record.cid,
      rsvp: record.value,
      event,
    });
  }

  return rsvpsWithEvents;
}

// RSVP status values from the lexicon
export const RSVP_STATUS = {
  GOING: "community.lexicon.calendar.rsvp#going",
  INTERESTED: "community.lexicon.calendar.rsvp#interested",
  NOT_GOING: "community.lexicon.calendar.rsvp#notgoing",
} as const;

export type RsvpStatus = (typeof RSVP_STATUS)[keyof typeof RSVP_STATUS];

/**
 * Creates or updates an RSVP for an event.
 * The RSVP is stored on the user's PDS and permissioned via the event's clique.
 * Uses a deterministic rkey derived from the event so updates overwrite the existing RSVP.
 *
 * @param client - The Habitat client instance
 * @param eventUri - The URI of the event (e.g., habitat://did:plc:xxx/community.lexicon.calendar.event/rkey)
 * @param status - The RSVP status
 * @returns The created/updated RSVP record response
 */
export async function createRsvp(
  client: HabitatClient,
  eventUri: string,
  status: RsvpStatus,
): Promise<PutPrivateRecordResponse> {
  const parsed = parseRecordUri(eventUri);
  if (!parsed) {
    throw new Error(`Invalid event URI: ${eventUri}`);
  }

  // Derive the clique URI from the event
  // The clique rkey follows the pattern: event-{eventRkey}
  const cliqueUri = buildCliqueUri(parsed.did, `event-${parsed.rkey}`);

  // Use a deterministic rkey so updating RSVP overwrites the existing one
  // Format: rsvp-for-{eventOwnerDid}-{eventRkey}
  const rsvpRkey = `rsvp-for-${parsed.did}-${parsed.rkey}`;
  const rsvpRecord: Omit<Rsvp, "$type"> = {
    subject: {
      uri: eventUri,
      cid: "", // Will be filled by server
    },
    status,
  };

  return client.putPrivateRecord(RSVP_COLLECTION, rsvpRecord, rsvpRkey, {
    cliques: [cliqueUri],
  });
}
