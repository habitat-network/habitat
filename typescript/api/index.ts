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
import * as ComAtprotoRepoGetRecord from './types/com/atproto/repo/getRecord.js'
import * as ComAtprotoRepoListRecords from './types/com/atproto/repo/listRecords.js'
import * as ComAtprotoRepoPutRecord from './types/com/atproto/repo/putRecord.js'
import * as ComAtprotoRepoStrongRef from './types/com/atproto/repo/strongRef.js'
import * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
import * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
import * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
import * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
import * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
import * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
import * as NetworkHabitatCliqueAddItem from './types/network/habitat/clique/addItem.js'
import * as NetworkHabitatInternalGetRecord from './types/network/habitat/internal/getRecord.js'
import * as NetworkHabitatInternalNotifyOfUpdate from './types/network/habitat/internal/notifyOfUpdate.js'
import * as NetworkHabitatPermissionsAddPermission from './types/network/habitat/permissions/addPermission.js'
import * as NetworkHabitatPermissionsListPermissions from './types/network/habitat/permissions/listPermissions.js'
import * as NetworkHabitatPermissionsRemovePermission from './types/network/habitat/permissions/removePermission.js'
import * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
import * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
import * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
import * as NetworkHabitatListRecords from './types/network/habitat/listRecords.js'
import * as NetworkHabitatRepoPutRecord from './types/network/habitat/repo/putRecord.js'
import * as NetworkHabitatRepoUploadBlob from './types/network/habitat/repo/uploadBlob.js'

export * as ComAtprotoRepoCreateRecord from './types/com/atproto/repo/createRecord.js'
export * as ComAtprotoRepoDefs from './types/com/atproto/repo/defs.js'
export * as ComAtprotoRepoDeleteRecord from './types/com/atproto/repo/deleteRecord.js'
export * as ComAtprotoRepoGetRecord from './types/com/atproto/repo/getRecord.js'
export * as ComAtprotoRepoListRecords from './types/com/atproto/repo/listRecords.js'
export * as ComAtprotoRepoPutRecord from './types/com/atproto/repo/putRecord.js'
export * as ComAtprotoRepoStrongRef from './types/com/atproto/repo/strongRef.js'
export * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
export * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
export * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
export * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
export * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
export * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
export * as NetworkHabitatCliqueAddItem from './types/network/habitat/clique/addItem.js'
export * as NetworkHabitatInternalGetRecord from './types/network/habitat/internal/getRecord.js'
export * as NetworkHabitatInternalNotifyOfUpdate from './types/network/habitat/internal/notifyOfUpdate.js'
export * as NetworkHabitatPermissionsAddPermission from './types/network/habitat/permissions/addPermission.js'
export * as NetworkHabitatPermissionsListPermissions from './types/network/habitat/permissions/listPermissions.js'
export * as NetworkHabitatPermissionsRemovePermission from './types/network/habitat/permissions/removePermission.js'
export * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
export * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
export * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
export * as NetworkHabitatListRecords from './types/network/habitat/listRecords.js'
export * as NetworkHabitatRepoPutRecord from './types/network/habitat/repo/putRecord.js'
export * as NetworkHabitatRepoUploadBlob from './types/network/habitat/repo/uploadBlob.js'

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

  constructor(client: XrpcClient) {
    this._client = client
    this.repo = new ComAtprotoRepoNS(client)
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
  rsvp: CommunityLexiconCalendarRsvpRecord

  constructor(client: XrpcClient) {
    this._client = client
    this.event = new CommunityLexiconCalendarEventRecord(client)
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
  photo: NetworkHabitatPhotoRecord
  clique: NetworkHabitatCliqueNS
  internal: NetworkHabitatInternalNS
  permissions: NetworkHabitatPermissionsNS
  repo: NetworkHabitatRepoNS

  constructor(client: XrpcClient) {
    this._client = client
    this.clique = new NetworkHabitatCliqueNS(client)
    this.internal = new NetworkHabitatInternalNS(client)
    this.permissions = new NetworkHabitatPermissionsNS(client)
    this.repo = new NetworkHabitatRepoNS(client)
    this.photo = new NetworkHabitatPhotoRecord(client)
  }

  listRecords(
    data?: NetworkHabitatListRecords.InputSchema,
    opts?: NetworkHabitatListRecords.CallOptions,
  ): Promise<NetworkHabitatListRecords.Response> {
    return this._client.call(
      'network.habitat.listRecords',
      opts?.qp,
      data,
      opts,
    )
  }
}

export class NetworkHabitatCliqueNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  addItem(
    data?: NetworkHabitatCliqueAddItem.InputSchema,
    opts?: NetworkHabitatCliqueAddItem.CallOptions,
  ): Promise<NetworkHabitatCliqueAddItem.Response> {
    return this._client.call(
      'network.habitat.clique.addItem',
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

  getRecord(
    params?: NetworkHabitatInternalGetRecord.QueryParams,
    opts?: NetworkHabitatInternalGetRecord.CallOptions,
  ): Promise<NetworkHabitatInternalGetRecord.Response> {
    return this._client
      .call('network.habitat.internal.getRecord', params, undefined, opts)
      .catch((e) => {
        throw NetworkHabitatInternalGetRecord.toKnownErr(e)
      })
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

export class NetworkHabitatRepoNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
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
