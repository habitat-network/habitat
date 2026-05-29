/**
 * GENERATED CODE - DO NOT MODIFY
 */
import {
  XrpcClient,
  type FetchHandler,
  type FetchHandlerOptions,
} from '@atproto/xrpc'
import { schemas } from './lexicons.js'
import { CID } from 'multiformats/cid'
import { type OmitKey, type Un$Typed } from './util.js'
import * as ComAtprotoRepoCreateRecord from './types/com/atproto/repo/createRecord.js'
import * as ComAtprotoRepoDefs from './types/com/atproto/repo/defs.js'
import * as ComAtprotoRepoDeleteRecord from './types/com/atproto/repo/deleteRecord.js'
import * as ComAtprotoRepoDescribeRepo from './types/com/atproto/repo/describeRepo.js'
import * as ComAtprotoRepoGetRecord from './types/com/atproto/repo/getRecord.js'
import * as ComAtprotoRepoListRecords from './types/com/atproto/repo/listRecords.js'
import * as ComAtprotoRepoPutRecord from './types/com/atproto/repo/putRecord.js'
import * as ComAtprotoRepoStrongRef from './types/com/atproto/repo/strongRef.js'
import * as ComAtprotoServerGetServiceAuth from './types/com/atproto/server/getServiceAuth.js'
import * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
import * as CommunityLexiconCalendarInvite from './types/community/lexicon/calendar/invite.js'
import * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
import * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
import * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
import * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
import * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
import * as NetworkHabitatClique from './types/network/habitat/clique.js'
import * as NetworkHabitatCliqueAddMembers from './types/network/habitat/clique/addMembers.js'
import * as NetworkHabitatCliqueCreateClique from './types/network/habitat/clique/createClique.js'
import * as NetworkHabitatCliqueGetMembers from './types/network/habitat/clique/getMembers.js'
import * as NetworkHabitatCliqueIsMember from './types/network/habitat/clique/isMember.js'
import * as NetworkHabitatCliqueRemoveMembers from './types/network/habitat/clique/removeMembers.js'
import * as NetworkHabitatDocs from './types/network/habitat/docs.js'
import * as NetworkHabitatGrantee from './types/network/habitat/grantee.js'
import * as NetworkHabitatInternalNotifyOfUpdate from './types/network/habitat/internal/notifyOfUpdate.js'
import * as NetworkHabitatListConnectedApps from './types/network/habitat/listConnectedApps.js'
import * as NetworkHabitatOrgAddAdmin from './types/network/habitat/org/addAdmin.js'
import * as NetworkHabitatOrgAddMembers from './types/network/habitat/org/addMembers.js'
import * as NetworkHabitatOrgCreate from './types/network/habitat/org/create.js'
import * as NetworkHabitatOrgDowngradeAdmin from './types/network/habitat/org/downgradeAdmin.js'
import * as NetworkHabitatOrgGetAdmins from './types/network/habitat/org/getAdmins.js'
import * as NetworkHabitatOrgGetMembers from './types/network/habitat/org/getMembers.js'
import * as NetworkHabitatOrgGetMetadata from './types/network/habitat/org/getMetadata.js'
import * as NetworkHabitatOrgIssueInviteToken from './types/network/habitat/org/issueInviteToken.js'
import * as NetworkHabitatOrgLoginMember from './types/network/habitat/org/loginMember.js'
import * as NetworkHabitatOrgMintMemberIdentity from './types/network/habitat/org/mintMemberIdentity.js'
import * as NetworkHabitatOrgRemoveAdmin from './types/network/habitat/org/removeAdmin.js'
import * as NetworkHabitatOrgRemoveMembers from './types/network/habitat/org/removeMembers.js'
import * as NetworkHabitatPermissionsAddPermission from './types/network/habitat/permissions/addPermission.js'
import * as NetworkHabitatPermissionsListPermissions from './types/network/habitat/permissions/listPermissions.js'
import * as NetworkHabitatPermissionsRemovePermission from './types/network/habitat/permissions/removePermission.js'
import * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
import * as NetworkHabitatRenderSchema from './types/network/habitat/render/schema.js'
import * as NetworkHabitatRepoCreateRecord from './types/network/habitat/repo/createRecord.js'
import * as NetworkHabitatRepoDeleteRecord from './types/network/habitat/repo/deleteRecord.js'
import * as NetworkHabitatRepoDescribeRepo from './types/network/habitat/repo/describeRepo.js'
import * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
import * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
import * as NetworkHabitatRepoListRecords from './types/network/habitat/repo/listRecords.js'
import * as NetworkHabitatRepoPutRecord from './types/network/habitat/repo/putRecord.js'
import * as NetworkHabitatRepoUploadBlob from './types/network/habitat/repo/uploadBlob.js'
import * as NetworkHabitatSpaceAddMember from './types/network/habitat/space/addMember.js'
import * as NetworkHabitatSpaceCreateSpace from './types/network/habitat/space/createSpace.js'
import * as NetworkHabitatSpaceDeleteRecord from './types/network/habitat/space/deleteRecord.js'
import * as NetworkHabitatSpaceDeleteSpace from './types/network/habitat/space/deleteSpace.js'
import * as NetworkHabitatSpaceGetMembers from './types/network/habitat/space/getMembers.js'
import * as NetworkHabitatSpaceGetRecord from './types/network/habitat/space/getRecord.js'
import * as NetworkHabitatSpaceListRecords from './types/network/habitat/space/listRecords.js'
import * as NetworkHabitatSpaceListSpaces from './types/network/habitat/space/listSpaces.js'
import * as NetworkHabitatSpacePutRecord from './types/network/habitat/space/putRecord.js'
import * as NetworkHabitatSpaceRemoveMember from './types/network/habitat/space/removeMember.js'

export * as ComAtprotoRepoCreateRecord from './types/com/atproto/repo/createRecord.js'
export * as ComAtprotoRepoDefs from './types/com/atproto/repo/defs.js'
export * as ComAtprotoRepoDeleteRecord from './types/com/atproto/repo/deleteRecord.js'
export * as ComAtprotoRepoDescribeRepo from './types/com/atproto/repo/describeRepo.js'
export * as ComAtprotoRepoGetRecord from './types/com/atproto/repo/getRecord.js'
export * as ComAtprotoRepoListRecords from './types/com/atproto/repo/listRecords.js'
export * as ComAtprotoRepoPutRecord from './types/com/atproto/repo/putRecord.js'
export * as ComAtprotoRepoStrongRef from './types/com/atproto/repo/strongRef.js'
export * as ComAtprotoServerGetServiceAuth from './types/com/atproto/server/getServiceAuth.js'
export * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
export * as CommunityLexiconCalendarInvite from './types/community/lexicon/calendar/invite.js'
export * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
export * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
export * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
export * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
export * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
export * as NetworkHabitatClique from './types/network/habitat/clique.js'
export * as NetworkHabitatCliqueAddMembers from './types/network/habitat/clique/addMembers.js'
export * as NetworkHabitatCliqueCreateClique from './types/network/habitat/clique/createClique.js'
export * as NetworkHabitatCliqueGetMembers from './types/network/habitat/clique/getMembers.js'
export * as NetworkHabitatCliqueIsMember from './types/network/habitat/clique/isMember.js'
export * as NetworkHabitatCliqueRemoveMembers from './types/network/habitat/clique/removeMembers.js'
export * as NetworkHabitatDocs from './types/network/habitat/docs.js'
export * as NetworkHabitatGrantee from './types/network/habitat/grantee.js'
export * as NetworkHabitatInternalNotifyOfUpdate from './types/network/habitat/internal/notifyOfUpdate.js'
export * as NetworkHabitatListConnectedApps from './types/network/habitat/listConnectedApps.js'
export * as NetworkHabitatOrgAddAdmin from './types/network/habitat/org/addAdmin.js'
export * as NetworkHabitatOrgAddMembers from './types/network/habitat/org/addMembers.js'
export * as NetworkHabitatOrgCreate from './types/network/habitat/org/create.js'
export * as NetworkHabitatOrgDowngradeAdmin from './types/network/habitat/org/downgradeAdmin.js'
export * as NetworkHabitatOrgGetAdmins from './types/network/habitat/org/getAdmins.js'
export * as NetworkHabitatOrgGetMembers from './types/network/habitat/org/getMembers.js'
export * as NetworkHabitatOrgGetMetadata from './types/network/habitat/org/getMetadata.js'
export * as NetworkHabitatOrgIssueInviteToken from './types/network/habitat/org/issueInviteToken.js'
export * as NetworkHabitatOrgLoginMember from './types/network/habitat/org/loginMember.js'
export * as NetworkHabitatOrgMintMemberIdentity from './types/network/habitat/org/mintMemberIdentity.js'
export * as NetworkHabitatOrgRemoveAdmin from './types/network/habitat/org/removeAdmin.js'
export * as NetworkHabitatOrgRemoveMembers from './types/network/habitat/org/removeMembers.js'
export * as NetworkHabitatPermissionsAddPermission from './types/network/habitat/permissions/addPermission.js'
export * as NetworkHabitatPermissionsListPermissions from './types/network/habitat/permissions/listPermissions.js'
export * as NetworkHabitatPermissionsRemovePermission from './types/network/habitat/permissions/removePermission.js'
export * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
export * as NetworkHabitatRenderSchema from './types/network/habitat/render/schema.js'
export * as NetworkHabitatRepoCreateRecord from './types/network/habitat/repo/createRecord.js'
export * as NetworkHabitatRepoDeleteRecord from './types/network/habitat/repo/deleteRecord.js'
export * as NetworkHabitatRepoDescribeRepo from './types/network/habitat/repo/describeRepo.js'
export * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
export * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
export * as NetworkHabitatRepoListRecords from './types/network/habitat/repo/listRecords.js'
export * as NetworkHabitatRepoPutRecord from './types/network/habitat/repo/putRecord.js'
export * as NetworkHabitatRepoUploadBlob from './types/network/habitat/repo/uploadBlob.js'
export * as NetworkHabitatSpaceAddMember from './types/network/habitat/space/addMember.js'
export * as NetworkHabitatSpaceCreateSpace from './types/network/habitat/space/createSpace.js'
export * as NetworkHabitatSpaceDeleteRecord from './types/network/habitat/space/deleteRecord.js'
export * as NetworkHabitatSpaceDeleteSpace from './types/network/habitat/space/deleteSpace.js'
export * as NetworkHabitatSpaceGetMembers from './types/network/habitat/space/getMembers.js'
export * as NetworkHabitatSpaceGetRecord from './types/network/habitat/space/getRecord.js'
export * as NetworkHabitatSpaceListRecords from './types/network/habitat/space/listRecords.js'
export * as NetworkHabitatSpaceListSpaces from './types/network/habitat/space/listSpaces.js'
export * as NetworkHabitatSpacePutRecord from './types/network/habitat/space/putRecord.js'
export * as NetworkHabitatSpaceRemoveMember from './types/network/habitat/space/removeMember.js'

export const COMMUNITY_LEXICON_CALENDAR = {
  EventVirtual: 'community.lexicon.calendar.event#virtual',
  EventInperson: 'community.lexicon.calendar.event#inperson',
  EventHybrid: 'community.lexicon.calendar.event#hybrid',
  EventPlanned: 'community.lexicon.calendar.event#planned',
  EventScheduled: 'community.lexicon.calendar.event#scheduled',
  EventRescheduled: 'community.lexicon.calendar.event#rescheduled',
  EventCancelled: 'community.lexicon.calendar.event#cancelled',
  EventPostponed: 'community.lexicon.calendar.event#postponed',
  RsvpInterested: 'community.lexicon.calendar.rsvp#interested',
  RsvpGoing: 'community.lexicon.calendar.rsvp#going',
  RsvpNotgoing: 'community.lexicon.calendar.rsvp#notgoing',
}
export const NETWORK_HABITAT_RENDER = {
  SchemaText: 'network.habitat.render.schema#text',
  SchemaDatetime: 'network.habitat.render.schema#datetime',
  SchemaUrl: 'network.habitat.render.schema#url',
  SchemaBadge: 'network.habitat.render.schema#badge',
  SchemaList: 'network.habitat.render.schema#list',
  SchemaPrimary: 'network.habitat.render.schema#primary',
  SchemaSecondary: 'network.habitat.render.schema#secondary',
  SchemaMetadata: 'network.habitat.render.schema#metadata',
}

export class AtpBaseClient extends XrpcClient {
  com: ComNS
  community: CommunityNS
  network: NetworkNS

  constructor(options: FetchHandler | FetchHandlerOptions) {
    super(options, schemas)
    this.com = new ComNS(this)
    this.community = new CommunityNS(this)
    this.network = new NetworkNS(this)
  }

  /** @deprecated use `this` instead */
  get xrpc(): XrpcClient {
    return this
  }
}

export class ComNS {
  _client: XrpcClient
  atproto: ComAtprotoNS

  constructor(client: XrpcClient) {
    this._client = client
    this.atproto = new ComAtprotoNS(client)
  }
}

export class ComAtprotoNS {
  _client: XrpcClient
  repo: ComAtprotoRepoNS
  server: ComAtprotoServerNS

  constructor(client: XrpcClient) {
    this._client = client
    this.repo = new ComAtprotoRepoNS(client)
    this.server = new ComAtprotoServerNS(client)
  }
}

export class ComAtprotoRepoNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  createRecord(
    data?: ComAtprotoRepoCreateRecord.InputSchema,
    opts?: ComAtprotoRepoCreateRecord.CallOptions,
  ): Promise<ComAtprotoRepoCreateRecord.Response> {
    return this._client
      .call('com.atproto.repo.createRecord', opts?.qp, data, opts)
      .catch((e) => {
        throw ComAtprotoRepoCreateRecord.toKnownErr(e)
      })
  }

  deleteRecord(
    data?: ComAtprotoRepoDeleteRecord.InputSchema,
    opts?: ComAtprotoRepoDeleteRecord.CallOptions,
  ): Promise<ComAtprotoRepoDeleteRecord.Response> {
    return this._client
      .call('com.atproto.repo.deleteRecord', opts?.qp, data, opts)
      .catch((e) => {
        throw ComAtprotoRepoDeleteRecord.toKnownErr(e)
      })
  }

  describeRepo(
    params?: ComAtprotoRepoDescribeRepo.QueryParams,
    opts?: ComAtprotoRepoDescribeRepo.CallOptions,
  ): Promise<ComAtprotoRepoDescribeRepo.Response> {
    return this._client.call(
      'com.atproto.repo.describeRepo',
      params,
      undefined,
      opts,
    )
  }

  getRecord(
    params?: ComAtprotoRepoGetRecord.QueryParams,
    opts?: ComAtprotoRepoGetRecord.CallOptions,
  ): Promise<ComAtprotoRepoGetRecord.Response> {
    return this._client
      .call('com.atproto.repo.getRecord', params, undefined, opts)
      .catch((e) => {
        throw ComAtprotoRepoGetRecord.toKnownErr(e)
      })
  }

  listRecords(
    params?: ComAtprotoRepoListRecords.QueryParams,
    opts?: ComAtprotoRepoListRecords.CallOptions,
  ): Promise<ComAtprotoRepoListRecords.Response> {
    return this._client.call(
      'com.atproto.repo.listRecords',
      params,
      undefined,
      opts,
    )
  }

  putRecord(
    data?: ComAtprotoRepoPutRecord.InputSchema,
    opts?: ComAtprotoRepoPutRecord.CallOptions,
  ): Promise<ComAtprotoRepoPutRecord.Response> {
    return this._client
      .call('com.atproto.repo.putRecord', opts?.qp, data, opts)
      .catch((e) => {
        throw ComAtprotoRepoPutRecord.toKnownErr(e)
      })
  }
}

export class ComAtprotoServerNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  getServiceAuth(
    params?: ComAtprotoServerGetServiceAuth.QueryParams,
    opts?: ComAtprotoServerGetServiceAuth.CallOptions,
  ): Promise<ComAtprotoServerGetServiceAuth.Response> {
    return this._client
      .call('com.atproto.server.getServiceAuth', params, undefined, opts)
      .catch((e) => {
        throw ComAtprotoServerGetServiceAuth.toKnownErr(e)
      })
  }
}

export class CommunityNS {
  _client: XrpcClient
  lexicon: CommunityLexiconNS

  constructor(client: XrpcClient) {
    this._client = client
    this.lexicon = new CommunityLexiconNS(client)
  }
}

export class CommunityLexiconNS {
  _client: XrpcClient
  calendar: CommunityLexiconCalendarNS
  location: CommunityLexiconLocationNS

  constructor(client: XrpcClient) {
    this._client = client
    this.calendar = new CommunityLexiconCalendarNS(client)
    this.location = new CommunityLexiconLocationNS(client)
  }
}

export class CommunityLexiconCalendarNS {
  _client: XrpcClient
  event: CommunityLexiconCalendarEventRecord
  invite: CommunityLexiconCalendarInviteRecord
  rsvp: CommunityLexiconCalendarRsvpRecord

  constructor(client: XrpcClient) {
    this._client = client
    this.event = new CommunityLexiconCalendarEventRecord(client)
    this.invite = new CommunityLexiconCalendarInviteRecord(client)
    this.rsvp = new CommunityLexiconCalendarRsvpRecord(client)
  }
}

export class CommunityLexiconCalendarEventRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: CommunityLexiconCalendarEvent.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'community.lexicon.calendar.event',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{
    uri: string
    cid: string
    value: CommunityLexiconCalendarEvent.Record
  }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'community.lexicon.calendar.event',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarEvent.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.event'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarEvent.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.event'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'community.lexicon.calendar.event', ...params },
      { headers },
    )
  }
}

export class CommunityLexiconCalendarInviteRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: CommunityLexiconCalendarInvite.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'community.lexicon.calendar.invite',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{
    uri: string
    cid: string
    value: CommunityLexiconCalendarInvite.Record
  }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'community.lexicon.calendar.invite',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarInvite.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.invite'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarInvite.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.invite'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'community.lexicon.calendar.invite', ...params },
      { headers },
    )
  }
}

export class CommunityLexiconCalendarRsvpRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: CommunityLexiconCalendarRsvp.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'community.lexicon.calendar.rsvp',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{
    uri: string
    cid: string
    value: CommunityLexiconCalendarRsvp.Record
  }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'community.lexicon.calendar.rsvp',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarRsvp.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.rsvp'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<CommunityLexiconCalendarRsvp.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'community.lexicon.calendar.rsvp'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'community.lexicon.calendar.rsvp', ...params },
      { headers },
    )
  }
}

export class CommunityLexiconLocationNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }
}

export class NetworkNS {
  _client: XrpcClient
  habitat: NetworkHabitatNS

  constructor(client: XrpcClient) {
    this._client = client
    this.habitat = new NetworkHabitatNS(client)
  }
}

export class NetworkHabitatNS {
  _client: XrpcClient
  docs: NetworkHabitatDocsRecord
  photo: NetworkHabitatPhotoRecord
  clique: NetworkHabitatCliqueNS
  internal: NetworkHabitatInternalNS
  org: NetworkHabitatOrgNS
  permissions: NetworkHabitatPermissionsNS
  render: NetworkHabitatRenderNS
  repo: NetworkHabitatRepoNS
  space: NetworkHabitatSpaceNS

  constructor(client: XrpcClient) {
    this._client = client
    this.clique = new NetworkHabitatCliqueNS(client)
    this.internal = new NetworkHabitatInternalNS(client)
    this.org = new NetworkHabitatOrgNS(client)
    this.permissions = new NetworkHabitatPermissionsNS(client)
    this.render = new NetworkHabitatRenderNS(client)
    this.repo = new NetworkHabitatRepoNS(client)
    this.space = new NetworkHabitatSpaceNS(client)
    this.docs = new NetworkHabitatDocsRecord(client)
    this.photo = new NetworkHabitatPhotoRecord(client)
  }

  listConnectedApps(
    params?: NetworkHabitatListConnectedApps.QueryParams,
    opts?: NetworkHabitatListConnectedApps.CallOptions,
  ): Promise<NetworkHabitatListConnectedApps.Response> {
    return this._client.call(
      'network.habitat.listConnectedApps',
      params,
      undefined,
      opts,
    )
  }
}

export class NetworkHabitatCliqueNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  addMembers(
    data?: NetworkHabitatCliqueAddMembers.InputSchema,
    opts?: NetworkHabitatCliqueAddMembers.CallOptions,
  ): Promise<NetworkHabitatCliqueAddMembers.Response> {
    return this._client.call(
      'network.habitat.clique.addMembers',
      opts?.qp,
      data,
      opts,
    )
  }

  createClique(
    data?: NetworkHabitatCliqueCreateClique.InputSchema,
    opts?: NetworkHabitatCliqueCreateClique.CallOptions,
  ): Promise<NetworkHabitatCliqueCreateClique.Response> {
    return this._client.call(
      'network.habitat.clique.createClique',
      opts?.qp,
      data,
      opts,
    )
  }

  getMembers(
    params?: NetworkHabitatCliqueGetMembers.QueryParams,
    opts?: NetworkHabitatCliqueGetMembers.CallOptions,
  ): Promise<NetworkHabitatCliqueGetMembers.Response> {
    return this._client.call(
      'network.habitat.clique.getMembers',
      params,
      undefined,
      opts,
    )
  }

  isMember(
    params?: NetworkHabitatCliqueIsMember.QueryParams,
    opts?: NetworkHabitatCliqueIsMember.CallOptions,
  ): Promise<NetworkHabitatCliqueIsMember.Response> {
    return this._client.call(
      'network.habitat.clique.isMember',
      params,
      undefined,
      opts,
    )
  }

  removeMembers(
    data?: NetworkHabitatCliqueRemoveMembers.InputSchema,
    opts?: NetworkHabitatCliqueRemoveMembers.CallOptions,
  ): Promise<NetworkHabitatCliqueRemoveMembers.Response> {
    return this._client.call(
      'network.habitat.clique.removeMembers',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatInternalNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  notifyOfUpdate(
    data?: NetworkHabitatInternalNotifyOfUpdate.InputSchema,
    opts?: NetworkHabitatInternalNotifyOfUpdate.CallOptions,
  ): Promise<NetworkHabitatInternalNotifyOfUpdate.Response> {
    return this._client.call(
      'network.habitat.internal.notifyOfUpdate',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatOrgNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  addAdmin(
    data?: NetworkHabitatOrgAddAdmin.InputSchema,
    opts?: NetworkHabitatOrgAddAdmin.CallOptions,
  ): Promise<NetworkHabitatOrgAddAdmin.Response> {
    return this._client.call(
      'network.habitat.org.addAdmin',
      opts?.qp,
      data,
      opts,
    )
  }

  addMembers(
    data?: NetworkHabitatOrgAddMembers.InputSchema,
    opts?: NetworkHabitatOrgAddMembers.CallOptions,
  ): Promise<NetworkHabitatOrgAddMembers.Response> {
    return this._client.call(
      'network.habitat.org.addMembers',
      opts?.qp,
      data,
      opts,
    )
  }

  create(
    data?: NetworkHabitatOrgCreate.InputSchema,
    opts?: NetworkHabitatOrgCreate.CallOptions,
  ): Promise<NetworkHabitatOrgCreate.Response> {
    return this._client.call('network.habitat.org.create', opts?.qp, data, opts)
  }

  downgradeAdmin(
    data?: NetworkHabitatOrgDowngradeAdmin.InputSchema,
    opts?: NetworkHabitatOrgDowngradeAdmin.CallOptions,
  ): Promise<NetworkHabitatOrgDowngradeAdmin.Response> {
    return this._client.call(
      'network.habitat.org.downgradeAdmin',
      opts?.qp,
      data,
      opts,
    )
  }

  getAdmins(
    params?: NetworkHabitatOrgGetAdmins.QueryParams,
    opts?: NetworkHabitatOrgGetAdmins.CallOptions,
  ): Promise<NetworkHabitatOrgGetAdmins.Response> {
    return this._client.call(
      'network.habitat.org.getAdmins',
      params,
      undefined,
      opts,
    )
  }

  getMembers(
    params?: NetworkHabitatOrgGetMembers.QueryParams,
    opts?: NetworkHabitatOrgGetMembers.CallOptions,
  ): Promise<NetworkHabitatOrgGetMembers.Response> {
    return this._client.call(
      'network.habitat.org.getMembers',
      params,
      undefined,
      opts,
    )
  }

  getMetadata(
    params?: NetworkHabitatOrgGetMetadata.QueryParams,
    opts?: NetworkHabitatOrgGetMetadata.CallOptions,
  ): Promise<NetworkHabitatOrgGetMetadata.Response> {
    return this._client.call(
      'network.habitat.org.getMetadata',
      params,
      undefined,
      opts,
    )
  }

  issueInviteToken(
    data?: NetworkHabitatOrgIssueInviteToken.InputSchema,
    opts?: NetworkHabitatOrgIssueInviteToken.CallOptions,
  ): Promise<NetworkHabitatOrgIssueInviteToken.Response> {
    return this._client.call(
      'network.habitat.org.issueInviteToken',
      opts?.qp,
      data,
      opts,
    )
  }

  loginMember(
    data?: NetworkHabitatOrgLoginMember.InputSchema,
    opts?: NetworkHabitatOrgLoginMember.CallOptions,
  ): Promise<NetworkHabitatOrgLoginMember.Response> {
    return this._client.call(
      'network.habitat.org.loginMember',
      opts?.qp,
      data,
      opts,
    )
  }

  mintMemberIdentity(
    data?: NetworkHabitatOrgMintMemberIdentity.InputSchema,
    opts?: NetworkHabitatOrgMintMemberIdentity.CallOptions,
  ): Promise<NetworkHabitatOrgMintMemberIdentity.Response> {
    return this._client.call(
      'network.habitat.org.mintMemberIdentity',
      opts?.qp,
      data,
      opts,
    )
  }

  removeAdmin(
    data?: NetworkHabitatOrgRemoveAdmin.InputSchema,
    opts?: NetworkHabitatOrgRemoveAdmin.CallOptions,
  ): Promise<NetworkHabitatOrgRemoveAdmin.Response> {
    return this._client.call(
      'network.habitat.org.removeAdmin',
      opts?.qp,
      data,
      opts,
    )
  }

  removeMembers(
    data?: NetworkHabitatOrgRemoveMembers.InputSchema,
    opts?: NetworkHabitatOrgRemoveMembers.CallOptions,
  ): Promise<NetworkHabitatOrgRemoveMembers.Response> {
    return this._client.call(
      'network.habitat.org.removeMembers',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatPermissionsNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  addPermission(
    data?: NetworkHabitatPermissionsAddPermission.InputSchema,
    opts?: NetworkHabitatPermissionsAddPermission.CallOptions,
  ): Promise<NetworkHabitatPermissionsAddPermission.Response> {
    return this._client.call(
      'network.habitat.permissions.addPermission',
      opts?.qp,
      data,
      opts,
    )
  }

  listPermissions(
    params?: NetworkHabitatPermissionsListPermissions.QueryParams,
    opts?: NetworkHabitatPermissionsListPermissions.CallOptions,
  ): Promise<NetworkHabitatPermissionsListPermissions.Response> {
    return this._client.call(
      'network.habitat.permissions.listPermissions',
      params,
      undefined,
      opts,
    )
  }

  removePermission(
    data?: NetworkHabitatPermissionsRemovePermission.InputSchema,
    opts?: NetworkHabitatPermissionsRemovePermission.CallOptions,
  ): Promise<NetworkHabitatPermissionsRemovePermission.Response> {
    return this._client.call(
      'network.habitat.permissions.removePermission',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatRenderNS {
  _client: XrpcClient
  schema: NetworkHabitatRenderSchemaRecord

  constructor(client: XrpcClient) {
    this._client = client
    this.schema = new NetworkHabitatRenderSchemaRecord(client)
  }
}

export class NetworkHabitatRenderSchemaRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: NetworkHabitatRenderSchema.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'network.habitat.render.schema',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{
    uri: string
    cid: string
    value: NetworkHabitatRenderSchema.Record
  }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'network.habitat.render.schema',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatRenderSchema.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.render.schema'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatRenderSchema.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.render.schema'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'network.habitat.render.schema', ...params },
      { headers },
    )
  }
}

export class NetworkHabitatRepoNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  createRecord(
    data?: NetworkHabitatRepoCreateRecord.InputSchema,
    opts?: NetworkHabitatRepoCreateRecord.CallOptions,
  ): Promise<NetworkHabitatRepoCreateRecord.Response> {
    return this._client.call(
      'network.habitat.repo.createRecord',
      opts?.qp,
      data,
      opts,
    )
  }

  deleteRecord(
    data?: NetworkHabitatRepoDeleteRecord.InputSchema,
    opts?: NetworkHabitatRepoDeleteRecord.CallOptions,
  ): Promise<NetworkHabitatRepoDeleteRecord.Response> {
    return this._client.call(
      'network.habitat.repo.deleteRecord',
      opts?.qp,
      data,
      opts,
    )
  }

  describeRepo(
    params?: NetworkHabitatRepoDescribeRepo.QueryParams,
    opts?: NetworkHabitatRepoDescribeRepo.CallOptions,
  ): Promise<NetworkHabitatRepoDescribeRepo.Response> {
    return this._client.call(
      'network.habitat.repo.describeRepo',
      params,
      undefined,
      opts,
    )
  }

  getBlob(
    params?: NetworkHabitatRepoGetBlob.QueryParams,
    opts?: NetworkHabitatRepoGetBlob.CallOptions,
  ): Promise<NetworkHabitatRepoGetBlob.Response> {
    return this._client
      .call('network.habitat.repo.getBlob', params, undefined, opts)
      .catch((e) => {
        throw NetworkHabitatRepoGetBlob.toKnownErr(e)
      })
  }

  getRecord(
    params?: NetworkHabitatRepoGetRecord.QueryParams,
    opts?: NetworkHabitatRepoGetRecord.CallOptions,
  ): Promise<NetworkHabitatRepoGetRecord.Response> {
    return this._client
      .call('network.habitat.repo.getRecord', params, undefined, opts)
      .catch((e) => {
        throw NetworkHabitatRepoGetRecord.toKnownErr(e)
      })
  }

  listRecords(
    params?: NetworkHabitatRepoListRecords.QueryParams,
    opts?: NetworkHabitatRepoListRecords.CallOptions,
  ): Promise<NetworkHabitatRepoListRecords.Response> {
    return this._client.call(
      'network.habitat.repo.listRecords',
      params,
      undefined,
      opts,
    )
  }

  putRecord(
    data?: NetworkHabitatRepoPutRecord.InputSchema,
    opts?: NetworkHabitatRepoPutRecord.CallOptions,
  ): Promise<NetworkHabitatRepoPutRecord.Response> {
    return this._client.call(
      'network.habitat.repo.putRecord',
      opts?.qp,
      data,
      opts,
    )
  }

  uploadBlob(
    data?: NetworkHabitatRepoUploadBlob.InputSchema,
    opts?: NetworkHabitatRepoUploadBlob.CallOptions,
  ): Promise<NetworkHabitatRepoUploadBlob.Response> {
    return this._client.call(
      'network.habitat.repo.uploadBlob',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatSpaceNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  addMember(
    data?: NetworkHabitatSpaceAddMember.InputSchema,
    opts?: NetworkHabitatSpaceAddMember.CallOptions,
  ): Promise<NetworkHabitatSpaceAddMember.Response> {
    return this._client
      .call('network.habitat.space.addMember', opts?.qp, data, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceAddMember.toKnownErr(e)
      })
  }

  createSpace(
    data?: NetworkHabitatSpaceCreateSpace.InputSchema,
    opts?: NetworkHabitatSpaceCreateSpace.CallOptions,
  ): Promise<NetworkHabitatSpaceCreateSpace.Response> {
    return this._client
      .call('network.habitat.space.createSpace', opts?.qp, data, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceCreateSpace.toKnownErr(e)
      })
  }

  deleteRecord(
    data?: NetworkHabitatSpaceDeleteRecord.InputSchema,
    opts?: NetworkHabitatSpaceDeleteRecord.CallOptions,
  ): Promise<NetworkHabitatSpaceDeleteRecord.Response> {
    return this._client
      .call('network.habitat.space.deleteRecord', opts?.qp, data, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceDeleteRecord.toKnownErr(e)
      })
  }

  deleteSpace(
    data?: NetworkHabitatSpaceDeleteSpace.InputSchema,
    opts?: NetworkHabitatSpaceDeleteSpace.CallOptions,
  ): Promise<NetworkHabitatSpaceDeleteSpace.Response> {
    return this._client
      .call('network.habitat.space.deleteSpace', opts?.qp, data, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceDeleteSpace.toKnownErr(e)
      })
  }

  getMembers(
    params?: NetworkHabitatSpaceGetMembers.QueryParams,
    opts?: NetworkHabitatSpaceGetMembers.CallOptions,
  ): Promise<NetworkHabitatSpaceGetMembers.Response> {
    return this._client
      .call('network.habitat.space.getMembers', params, undefined, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceGetMembers.toKnownErr(e)
      })
  }

  getRecord(
    params?: NetworkHabitatSpaceGetRecord.QueryParams,
    opts?: NetworkHabitatSpaceGetRecord.CallOptions,
  ): Promise<NetworkHabitatSpaceGetRecord.Response> {
    return this._client
      .call('network.habitat.space.getRecord', params, undefined, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceGetRecord.toKnownErr(e)
      })
  }

  listRecords(
    params?: NetworkHabitatSpaceListRecords.QueryParams,
    opts?: NetworkHabitatSpaceListRecords.CallOptions,
  ): Promise<NetworkHabitatSpaceListRecords.Response> {
    return this._client.call(
      'network.habitat.space.listRecords',
      params,
      undefined,
      opts,
    )
  }

  listSpaces(
    params?: NetworkHabitatSpaceListSpaces.QueryParams,
    opts?: NetworkHabitatSpaceListSpaces.CallOptions,
  ): Promise<NetworkHabitatSpaceListSpaces.Response> {
    return this._client.call(
      'network.habitat.space.listSpaces',
      params,
      undefined,
      opts,
    )
  }

  putRecord(
    data?: NetworkHabitatSpacePutRecord.InputSchema,
    opts?: NetworkHabitatSpacePutRecord.CallOptions,
  ): Promise<NetworkHabitatSpacePutRecord.Response> {
    return this._client.call(
      'network.habitat.space.putRecord',
      opts?.qp,
      data,
      opts,
    )
  }

  removeMember(
    data?: NetworkHabitatSpaceRemoveMember.InputSchema,
    opts?: NetworkHabitatSpaceRemoveMember.CallOptions,
  ): Promise<NetworkHabitatSpaceRemoveMember.Response> {
    return this._client
      .call('network.habitat.space.removeMember', opts?.qp, data, opts)
      .catch((e) => {
        throw NetworkHabitatSpaceRemoveMember.toKnownErr(e)
      })
  }
}

export class NetworkHabitatDocsRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: NetworkHabitatDocs.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'network.habitat.docs',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{ uri: string; cid: string; value: NetworkHabitatDocs.Record }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'network.habitat.docs',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatDocs.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.docs'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatDocs.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.docs'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'network.habitat.docs', ...params },
      { headers },
    )
  }
}

export class NetworkHabitatPhotoRecord {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  async list(
    params: OmitKey<ComAtprotoRepoListRecords.QueryParams, 'collection'>,
  ): Promise<{
    cursor?: string
    records: { uri: string; value: NetworkHabitatPhoto.Record }[]
  }> {
    const res = await this._client.call('com.atproto.repo.listRecords', {
      collection: 'network.habitat.photo',
      ...params,
    })
    return res.data
  }

  async get(
    params: OmitKey<ComAtprotoRepoGetRecord.QueryParams, 'collection'>,
  ): Promise<{ uri: string; cid: string; value: NetworkHabitatPhoto.Record }> {
    const res = await this._client.call('com.atproto.repo.getRecord', {
      collection: 'network.habitat.photo',
      ...params,
    })
    return res.data
  }

  async create(
    params: OmitKey<
      ComAtprotoRepoCreateRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatPhoto.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.photo'
    const res = await this._client.call(
      'com.atproto.repo.createRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async put(
    params: OmitKey<
      ComAtprotoRepoPutRecord.InputSchema,
      'collection' | 'record'
    >,
    record: Un$Typed<NetworkHabitatPhoto.Record>,
    headers?: Record<string, string>,
  ): Promise<{ uri: string; cid: string }> {
    const collection = 'network.habitat.photo'
    const res = await this._client.call(
      'com.atproto.repo.putRecord',
      undefined,
      { collection, ...params, record: { ...record, $type: collection } },
      { encoding: 'application/json', headers },
    )
    return res.data
  }

  async delete(
    params: OmitKey<ComAtprotoRepoDeleteRecord.InputSchema, 'collection'>,
    headers?: Record<string, string>,
  ): Promise<void> {
    await this._client.call(
      'com.atproto.repo.deleteRecord',
      undefined,
      { collection: 'network.habitat.photo', ...params },
      { headers },
    )
  }
}
