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
import * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
import * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
import * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
import * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
import * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
import * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
import * as NetworkHabitatNotificationCreateNotification from './types/network/habitat/notification/createNotification.js'
import * as NetworkHabitatNotificationDefs from './types/network/habitat/notification/defs.js'
import * as NetworkHabitatNotificationListNotifications from './types/network/habitat/notification/listNotifications.js'
import * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
import * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
import * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
import * as NetworkHabitatRepoListRecords from './types/network/habitat/repo/listRecords.js'
import * as NetworkHabitatRepoPutRecord from './types/network/habitat/repo/putRecord.js'
import * as NetworkHabitatRepoUploadBlob from './types/network/habitat/repo/uploadBlob.js'

export * as CommunityLexiconCalendarEvent from './types/community/lexicon/calendar/event.js'
export * as CommunityLexiconCalendarRsvp from './types/community/lexicon/calendar/rsvp.js'
export * as CommunityLexiconLocationAddress from './types/community/lexicon/location/address.js'
export * as CommunityLexiconLocationFsq from './types/community/lexicon/location/fsq.js'
export * as CommunityLexiconLocationGeo from './types/community/lexicon/location/geo.js'
export * as CommunityLexiconLocationHthree from './types/community/lexicon/location/hthree.js'
export * as NetworkHabitatNotificationCreateNotification from './types/network/habitat/notification/createNotification.js'
export * as NetworkHabitatNotificationDefs from './types/network/habitat/notification/defs.js'
export * as NetworkHabitatNotificationListNotifications from './types/network/habitat/notification/listNotifications.js'
export * as NetworkHabitatPhoto from './types/network/habitat/photo.js'
export * as NetworkHabitatRepoGetBlob from './types/network/habitat/repo/getBlob.js'
export * as NetworkHabitatRepoGetRecord from './types/network/habitat/repo/getRecord.js'
export * as NetworkHabitatRepoListRecords from './types/network/habitat/repo/listRecords.js'
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
  community: CommunityNS
  network: NetworkNS

  constructor(options: FetchHandler | FetchHandlerOptions) {
    super(options, schemas)
    this.community = new CommunityNS(this)
    this.network = new NetworkNS(this)
  }

  /** @deprecated use `this` instead */
  get xrpc(): XrpcClient {
    return this
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
  notification: NetworkHabitatNotificationNS
  repo: NetworkHabitatRepoNS

  constructor(client: XrpcClient) {
    this._client = client
    this.notification = new NetworkHabitatNotificationNS(client)
    this.repo = new NetworkHabitatRepoNS(client)
    this.photo = new NetworkHabitatPhotoRecord(client)
  }
}

export class NetworkHabitatNotificationNS {
  _client: XrpcClient

  constructor(client: XrpcClient) {
    this._client = client
  }

  createNotification(
    data?: NetworkHabitatNotificationCreateNotification.InputSchema,
    opts?: NetworkHabitatNotificationCreateNotification.CallOptions,
  ): Promise<NetworkHabitatNotificationCreateNotification.Response> {
    return this._client.call(
      'network.habitat.notification.createNotification',
      opts?.qp,
      data,
      opts,
    )
  }

  listNotifications(
    params?: NetworkHabitatNotificationListNotifications.QueryParams,
    opts?: NetworkHabitatNotificationListNotifications.CallOptions,
  ): Promise<NetworkHabitatNotificationListNotifications.Response> {
    return this._client.call(
      'network.habitat.notification.listNotifications',
      params,
      undefined,
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
