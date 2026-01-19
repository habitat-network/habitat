/**
 * GENERATED CODE - DO NOT MODIFY
 */
import {
  type LexiconDoc,
  Lexicons,
  ValidationError,
  type ValidationResult,
} from '@atproto/lexicon'
import { type $Typed, is$typed, maybe$typed } from './util.js'

export const schemaDict = {
  ComAtprotoRepoCreateRecord: {
    lexicon: 1,
    id: 'com.atproto.repo.createRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Create a single new repository record. Requires auth, implemented by PDS.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repo', 'collection', 'record'],
            properties: {
              repo: {
                type: 'string',
                format: 'at-identifier',
                description:
                  'The handle or DID of the repo (aka, current account).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description: 'The NSID of the record collection.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description: 'The Record Key.',
                maxLength: 512,
              },
              validate: {
                type: 'boolean',
                description:
                  "Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.",
              },
              record: {
                type: 'unknown',
                description: 'The record itself. Must contain a $type field.',
              },
              swapCommit: {
                type: 'string',
                format: 'cid',
                description:
                  'Compare and swap with the previous commit by CID.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri', 'cid'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              cid: {
                type: 'string',
                format: 'cid',
              },
              commit: {
                type: 'ref',
                ref: 'lex:com.atproto.repo.defs#commitMeta',
              },
              validationStatus: {
                type: 'string',
                knownValues: ['valid', 'unknown'],
              },
            },
          },
        },
        errors: [
          {
            name: 'InvalidSwap',
            description:
              "Indicates that 'swapCommit' didn't match current repo commit.",
          },
        ],
      },
    },
  },
  ComAtprotoRepoDefs: {
    lexicon: 1,
    id: 'com.atproto.repo.defs',
    defs: {
      commitMeta: {
        type: 'object',
        required: ['cid', 'rev'],
        properties: {
          cid: {
            type: 'string',
            format: 'cid',
          },
          rev: {
            type: 'string',
            format: 'tid',
          },
        },
      },
    },
  },
  ComAtprotoRepoDeleteRecord: {
    lexicon: 1,
    id: 'com.atproto.repo.deleteRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Delete a repository record, or ensure it doesn't exist. Requires auth, implemented by PDS.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repo', 'collection', 'rkey'],
            properties: {
              repo: {
                type: 'string',
                format: 'at-identifier',
                description:
                  'The handle or DID of the repo (aka, current account).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description: 'The NSID of the record collection.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description: 'The Record Key.',
              },
              swapRecord: {
                type: 'string',
                format: 'cid',
                description:
                  'Compare and swap with the previous record by CID.',
              },
              swapCommit: {
                type: 'string',
                format: 'cid',
                description:
                  'Compare and swap with the previous commit by CID.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              commit: {
                type: 'ref',
                ref: 'lex:com.atproto.repo.defs#commitMeta',
              },
            },
          },
        },
        errors: [
          {
            name: 'InvalidSwap',
          },
        ],
      },
    },
  },
  ComAtprotoRepoGetRecord: {
    lexicon: 1,
    id: 'com.atproto.repo.getRecord',
    defs: {
      main: {
        type: 'query',
        description:
          'Get a single record from a repository. Does not require auth.',
        parameters: {
          type: 'params',
          required: ['repo', 'collection', 'rkey'],
          properties: {
            repo: {
              type: 'string',
              format: 'at-identifier',
              description: 'The handle or DID of the repo.',
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record collection.',
            },
            rkey: {
              type: 'string',
              description: 'The Record Key.',
              format: 'record-key',
            },
            cid: {
              type: 'string',
              format: 'cid',
              description:
                'The CID of the version of the record. If not specified, then return the most recent version.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri', 'value'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              cid: {
                type: 'string',
                format: 'cid',
              },
              value: {
                type: 'unknown',
              },
            },
          },
        },
        errors: [
          {
            name: 'RecordNotFound',
          },
        ],
      },
    },
  },
  ComAtprotoRepoListRecords: {
    lexicon: 1,
    id: 'com.atproto.repo.listRecords',
    defs: {
      main: {
        type: 'query',
        description:
          'List a range of records in a repository, matching a specific collection. Does not require auth.',
        parameters: {
          type: 'params',
          required: ['repo', 'collection'],
          properties: {
            repo: {
              type: 'string',
              format: 'at-identifier',
              description: 'The handle or DID of the repo.',
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record type.',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 100,
              default: 50,
              description: 'The number of records to return.',
            },
            cursor: {
              type: 'string',
            },
            reverse: {
              type: 'boolean',
              description: 'Flag to reverse the order of the returned records.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['records'],
            properties: {
              cursor: {
                type: 'string',
              },
              records: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:com.atproto.repo.listRecords#record',
                },
              },
            },
          },
        },
      },
      record: {
        type: 'object',
        required: ['uri', 'cid', 'value'],
        properties: {
          uri: {
            type: 'string',
            format: 'at-uri',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
          value: {
            type: 'unknown',
          },
        },
      },
    },
  },
  ComAtprotoRepoPutRecord: {
    lexicon: 1,
    id: 'com.atproto.repo.putRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Write a repository record, creating or updating it as needed. Requires auth, implemented by PDS.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repo', 'collection', 'rkey', 'record'],
            nullable: ['swapRecord'],
            properties: {
              repo: {
                type: 'string',
                format: 'at-identifier',
                description:
                  'The handle or DID of the repo (aka, current account).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description: 'The NSID of the record collection.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description: 'The Record Key.',
                maxLength: 512,
              },
              validate: {
                type: 'boolean',
                description:
                  "Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.",
              },
              record: {
                type: 'unknown',
                description: 'The record to write.',
              },
              swapRecord: {
                type: 'string',
                format: 'cid',
                description:
                  'Compare and swap with the previous record by CID. WARNING: nullable and optional field; may cause problems with golang implementation',
              },
              swapCommit: {
                type: 'string',
                format: 'cid',
                description:
                  'Compare and swap with the previous commit by CID.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri', 'cid'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              cid: {
                type: 'string',
                format: 'cid',
              },
              commit: {
                type: 'ref',
                ref: 'lex:com.atproto.repo.defs#commitMeta',
              },
              validationStatus: {
                type: 'string',
                knownValues: ['valid', 'unknown'],
              },
            },
          },
        },
        errors: [
          {
            name: 'InvalidSwap',
          },
        ],
      },
    },
  },
  ComAtprotoRepoStrongRef: {
    lexicon: 1,
    id: 'com.atproto.repo.strongRef',
    description: 'A URI with a content-hash fingerprint.',
    defs: {
      main: {
        type: 'object',
        required: ['uri', 'cid'],
        properties: {
          uri: {
            type: 'string',
            format: 'at-uri',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
        },
      },
    },
  },
  CommunityLexiconCalendarEvent: {
    lexicon: 1,
    id: 'community.lexicon.calendar.event',
    defs: {
      main: {
        type: 'record',
        description: 'A calendar event.',
        key: 'tid',
        record: {
          type: 'object',
          required: ['createdAt', 'name'],
          properties: {
            name: {
              type: 'string',
              description: 'The name of the event.',
            },
            description: {
              type: 'string',
              description: 'The description of the event.',
            },
            createdAt: {
              type: 'string',
              format: 'datetime',
              description:
                'Client-declared timestamp when the event was created.',
            },
            startsAt: {
              type: 'string',
              format: 'datetime',
              description: 'Client-declared timestamp when the event starts.',
            },
            endsAt: {
              type: 'string',
              format: 'datetime',
              description: 'Client-declared timestamp when the event ends.',
            },
            mode: {
              type: 'ref',
              ref: 'lex:community.lexicon.calendar.event#mode',
              description: 'The attendance mode of the event.',
            },
            status: {
              type: 'ref',
              ref: 'lex:community.lexicon.calendar.event#status',
              description: 'The status of the event.',
            },
            locations: {
              type: 'array',
              description: 'The locations where the event takes place.',
              items: {
                type: 'union',
                refs: [
                  'lex:community.lexicon.calendar.event#uri',
                  'lex:community.lexicon.location.address',
                  'lex:community.lexicon.location.fsq',
                  'lex:community.lexicon.location.geo',
                  'lex:community.lexicon.location.hthree',
                ],
              },
            },
            uris: {
              type: 'array',
              description: 'URIs associated with the event.',
              items: {
                type: 'ref',
                ref: 'lex:community.lexicon.calendar.event#uri',
              },
            },
          },
        },
      },
      mode: {
        type: 'string',
        description: 'The mode of the event.',
        default: 'community.lexicon.calendar.event#inperson',
        knownValues: [
          'community.lexicon.calendar.event#hybrid',
          'community.lexicon.calendar.event#inperson',
          'community.lexicon.calendar.event#virtual',
        ],
      },
      virtual: {
        type: 'token',
        description: 'A virtual event that takes place online.',
      },
      inperson: {
        type: 'token',
        description: 'An in-person event that takes place offline.',
      },
      hybrid: {
        type: 'token',
        description: 'A hybrid event that takes place both online and offline.',
      },
      status: {
        type: 'string',
        description: 'The status of the event.',
        default: 'community.lexicon.calendar.event#scheduled',
        knownValues: [
          'community.lexicon.calendar.event#cancelled',
          'community.lexicon.calendar.event#planned',
          'community.lexicon.calendar.event#postponed',
          'community.lexicon.calendar.event#rescheduled',
          'community.lexicon.calendar.event#scheduled',
        ],
      },
      planned: {
        type: 'token',
        description: 'The event has been created, but not finalized.',
      },
      scheduled: {
        type: 'token',
        description: 'The event has been created and scheduled.',
      },
      rescheduled: {
        type: 'token',
        description: 'The event has been rescheduled.',
      },
      cancelled: {
        type: 'token',
        description: 'The event has been cancelled.',
      },
      postponed: {
        type: 'token',
        description:
          'The event has been postponed and a new start date has not been set.',
      },
      uri: {
        type: 'object',
        description: 'A URI associated with the event.',
        required: ['uri'],
        properties: {
          uri: {
            type: 'string',
            format: 'uri',
          },
          name: {
            type: 'string',
            description: 'The display name of the URI.',
          },
        },
      },
    },
  },
  CommunityLexiconCalendarRsvp: {
    lexicon: 1,
    id: 'community.lexicon.calendar.rsvp',
    defs: {
      main: {
        type: 'record',
        description: 'An RSVP for an event.',
        key: 'tid',
        record: {
          type: 'object',
          required: ['subject', 'status'],
          properties: {
            subject: {
              type: 'ref',
              ref: 'lex:com.atproto.repo.strongRef',
            },
            status: {
              type: 'string',
              default: 'community.lexicon.calendar.rsvp#going',
              knownValues: [
                'community.lexicon.calendar.rsvp#interested',
                'community.lexicon.calendar.rsvp#going',
                'community.lexicon.calendar.rsvp#notgoing',
              ],
            },
          },
        },
      },
      interested: {
        type: 'token',
        description: 'Interested in the event',
      },
      going: {
        type: 'token',
        description: 'Going to the event',
      },
      notgoing: {
        type: 'token',
        description: 'Not going to the event',
      },
    },
  },
  CommunityLexiconLocationAddress: {
    lexicon: 1,
    id: 'community.lexicon.location.address',
    defs: {
      main: {
        type: 'object',
        description: 'A physical location in the form of a street address.',
        required: ['country'],
        properties: {
          country: {
            type: 'string',
            description:
              'The ISO 3166 country code. Preferably the 2-letter code.',
            minLength: 2,
            maxLength: 10,
          },
          postalCode: {
            type: 'string',
            description: 'The postal code of the location.',
          },
          region: {
            type: 'string',
            description:
              'The administrative region of the country. For example, a state in the USA.',
          },
          locality: {
            type: 'string',
            description:
              'The locality of the region. For example, a city in the USA.',
          },
          street: {
            type: 'string',
            description: 'The street address.',
          },
          name: {
            type: 'string',
            description: 'The name of the location.',
          },
        },
      },
    },
  },
  CommunityLexiconLocationFsq: {
    lexicon: 1,
    id: 'community.lexicon.location.fsq',
    defs: {
      main: {
        type: 'object',
        description:
          'A physical location contained in the Foursquare Open Source Places dataset.',
        required: ['fsq_place_id'],
        properties: {
          fsq_place_id: {
            type: 'string',
            description: 'The unique identifier of a Foursquare POI.',
          },
          latitude: {
            type: 'string',
          },
          longitude: {
            type: 'string',
          },
          name: {
            type: 'string',
            description: 'The name of the location.',
          },
        },
      },
    },
  },
  CommunityLexiconLocationGeo: {
    lexicon: 1,
    id: 'community.lexicon.location.geo',
    defs: {
      main: {
        type: 'object',
        description: 'A physical location in the form of a WGS84 coordinate.',
        required: ['latitude', 'longitude'],
        properties: {
          latitude: {
            type: 'string',
          },
          longitude: {
            type: 'string',
          },
          altitude: {
            type: 'string',
          },
          name: {
            type: 'string',
            description: 'The name of the location.',
          },
        },
      },
    },
  },
  CommunityLexiconLocationHthree: {
    lexicon: 1,
    id: 'community.lexicon.location.hthree',
    defs: {
      main: {
        type: 'object',
        description:
          'A physical location in the form of a H3 encoded location.',
        required: ['value'],
        properties: {
          value: {
            type: 'string',
            description: 'The h3 encoded location.',
          },
          name: {
            type: 'string',
            description: 'The name of the location.',
          },
        },
      },
    },
  },
  NetworkHabitatNotificationCreateNotification: {
    lexicon: 1,
    id: 'network.habitat.notification.createNotification',
    defs: {
      main: {
        type: 'procedure',
        description: 'Write a new notification.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repo', 'collection', 'record'],
            nullable: ['swapRecord'],
            properties: {
              repo: {
                type: 'string',
                format: 'did',
                description:
                  'The handle or DID of the repo (aka, current account).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description: 'The NSID of the record collection.',
              },
              record: {
                type: 'ref',
                description: 'The record to write.',
                ref: 'lex:network.habitat.notification.defs#notification',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              validationStatus: {
                type: 'string',
                knownValues: ['valid', 'unknown'],
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatNotificationDefs: {
    lexicon: 1,
    id: 'network.habitat.notification.defs',
    defs: {
      notification: {
        type: 'object',
        required: ['did', 'originDid', 'collection', 'rkey'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
            description: 'The handle or DID of the target of the notification.',
          },
          originDid: {
            type: 'string',
            format: 'did',
            description: 'The handle or DID of the origin of the notification.',
          },
          collection: {
            type: 'string',
            format: 'nsid',
            description: 'The NSID of the record collection.',
          },
          rkey: {
            type: 'string',
            format: 'record-key',
            description: 'The Record Key.',
            maxLength: 512,
          },
        },
      },
    },
  },
  NetworkHabitatNotificationListNotifications: {
    lexicon: 1,
    id: 'network.habitat.notification.listNotifications',
    defs: {
      main: {
        type: 'query',
        description: 'List a range of notifications for a given DID',
        parameters: {
          type: 'params',
          properties: {
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record type.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['records'],
            properties: {
              cursor: {
                type: 'string',
              },
              records: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.notification.listNotifications#record',
                },
              },
            },
          },
        },
      },
      record: {
        type: 'object',
        required: ['uri', 'cid', 'value'],
        properties: {
          uri: {
            type: 'string',
            format: 'at-uri',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
          value: {
            type: 'ref',
            ref: 'lex:network.habitat.notification.defs#notification',
          },
        },
      },
    },
  },
  NetworkHabitatPhoto: {
    lexicon: 1,
    id: 'network.habitat.photo',
    defs: {
      main: {
        type: 'record',
        description: 'An image upload.',
        key: 'tid',
        record: {
          type: 'object',
          required: ['ref'],
          properties: {
            ref: {
              type: 'blob',
            },
            createdAt: {
              type: 'string',
              format: 'datetime',
            },
          },
        },
      },
    },
  },
  NetworkHabitatRepoGetBlob: {
    lexicon: 1,
    id: 'network.habitat.repo.getBlob',
    defs: {
      main: {
        type: 'query',
        description:
          'Get a blob associated with a given account. Returns the full blob as originally uploaded. Does not require auth; implemented by PDS.',
        parameters: {
          type: 'params',
          required: ['did', 'cid'],
          properties: {
            did: {
              type: 'string',
              format: 'did',
              description: 'The DID of the account.',
            },
            cid: {
              type: 'string',
              format: 'cid',
              description: 'The CID of the blob to fetch',
            },
          },
        },
        output: {
          encoding: '*/*',
        },
        errors: [
          {
            name: 'BlobNotFound',
          },
          {
            name: 'RepoNotFound',
          },
          {
            name: 'RepoTakendown',
          },
          {
            name: 'RepoSuspended',
          },
          {
            name: 'RepoDeactivated',
          },
        ],
      },
    },
  },
  NetworkHabitatRepoGetRecord: {
    lexicon: 1,
    id: 'network.habitat.repo.getRecord',
    defs: {
      main: {
        type: 'query',
        description: 'Get a single record from a repository',
        parameters: {
          type: 'params',
          required: ['repo', 'collection', 'rkey'],
          properties: {
            repo: {
              type: 'string',
              format: 'at-identifier',
              description: 'The handle or DID of the repo.',
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record collection.',
            },
            rkey: {
              type: 'string',
              description: 'The Record Key.',
              format: 'record-key',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri', 'value'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              value: {
                type: 'unknown',
              },
            },
          },
        },
        errors: [
          {
            name: 'RecordNotFound',
          },
        ],
      },
    },
  },
  NetworkHabitatRepoListRecords: {
    lexicon: 1,
    id: 'network.habitat.repo.listRecords',
    defs: {
      main: {
        type: 'query',
        description:
          'List a range of records in a repository, matching a specific collection',
        parameters: {
          type: 'params',
          required: ['repo', 'collection'],
          properties: {
            repo: {
              type: 'string',
              format: 'at-identifier',
              description: 'The handle or DID of the repo.',
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record type.',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 100,
              default: 50,
              description: 'The number of records to return.',
            },
            cursor: {
              type: 'string',
            },
            reverse: {
              type: 'boolean',
              description: 'Flag to reverse the order of the returned records.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['records'],
            properties: {
              cursor: {
                type: 'string',
              },
              records: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.repo.listRecords#record',
                },
              },
            },
          },
        },
      },
      record: {
        type: 'object',
        required: ['uri', 'cid', 'value'],
        properties: {
          uri: {
            type: 'string',
            format: 'at-uri',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
          value: {
            type: 'unknown',
          },
        },
      },
    },
  },
  NetworkHabitatRepoPutRecord: {
    lexicon: 1,
    id: 'network.habitat.repo.putRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Write a repository record, creating or updating it as needed.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repo', 'collection', 'rkey', 'record'],
            nullable: ['swapRecord'],
            properties: {
              repo: {
                type: 'string',
                format: 'at-identifier',
                description:
                  'The handle or DID of the repo (aka, current account).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description: 'The NSID of the record collection.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description: 'The Record Key.',
                maxLength: 512,
              },
              validate: {
                type: 'boolean',
                description:
                  "Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.",
              },
              record: {
                type: 'unknown',
                description: 'The record to write.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
              },
              validationStatus: {
                type: 'string',
                knownValues: ['valid', 'unknown'],
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatRepoUploadBlob: {
    lexicon: 1,
    id: 'network.habitat.repo.uploadBlob',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Upload a new blob, to be referenced from a repository record. The blob will be deleted if it is not referenced within a time window (eg, minutes). Blob restrictions (mimetype, size, etc) are enforced when the reference is created. Requires auth, implemented by PDS.',
        input: {
          encoding: '*/*',
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['blob'],
            properties: {
              blob: {
                type: 'blob',
              },
              cid: {
                type: 'string',
                format: 'cid',
              },
            },
          },
        },
      },
    },
  },
} as const satisfies Record<string, LexiconDoc>
export const schemas = Object.values(schemaDict) satisfies LexiconDoc[]
export const lexicons: Lexicons = new Lexicons(schemas)

export function validate<T extends { $type: string }>(
  v: unknown,
  id: string,
  hash: string,
  requiredType: true,
): ValidationResult<T>
export function validate<T extends { $type?: string }>(
  v: unknown,
  id: string,
  hash: string,
  requiredType?: false,
): ValidationResult<T>
export function validate(
  v: unknown,
  id: string,
  hash: string,
  requiredType?: boolean,
): ValidationResult {
  return (requiredType ? is$typed : maybe$typed)(v, id, hash)
    ? lexicons.validate(`${id}#${hash}`, v)
    : {
        success: false,
        error: new ValidationError(
          `Must be an object with "${hash === 'main' ? id : `${id}#${hash}`}" $type property`,
        ),
      }
}

export const ids = {
  ComAtprotoRepoCreateRecord: 'com.atproto.repo.createRecord',
  ComAtprotoRepoDefs: 'com.atproto.repo.defs',
  ComAtprotoRepoDeleteRecord: 'com.atproto.repo.deleteRecord',
  ComAtprotoRepoGetRecord: 'com.atproto.repo.getRecord',
  ComAtprotoRepoListRecords: 'com.atproto.repo.listRecords',
  ComAtprotoRepoPutRecord: 'com.atproto.repo.putRecord',
  ComAtprotoRepoStrongRef: 'com.atproto.repo.strongRef',
  CommunityLexiconCalendarEvent: 'community.lexicon.calendar.event',
  CommunityLexiconCalendarRsvp: 'community.lexicon.calendar.rsvp',
  CommunityLexiconLocationAddress: 'community.lexicon.location.address',
  CommunityLexiconLocationFsq: 'community.lexicon.location.fsq',
  CommunityLexiconLocationGeo: 'community.lexicon.location.geo',
  CommunityLexiconLocationHthree: 'community.lexicon.location.hthree',
  NetworkHabitatNotificationCreateNotification:
    'network.habitat.notification.createNotification',
  NetworkHabitatNotificationDefs: 'network.habitat.notification.defs',
  NetworkHabitatNotificationListNotifications:
    'network.habitat.notification.listNotifications',
  NetworkHabitatPhoto: 'network.habitat.photo',
  NetworkHabitatRepoGetBlob: 'network.habitat.repo.getBlob',
  NetworkHabitatRepoGetRecord: 'network.habitat.repo.getRecord',
  NetworkHabitatRepoListRecords: 'network.habitat.repo.listRecords',
  NetworkHabitatRepoPutRecord: 'network.habitat.repo.putRecord',
  NetworkHabitatRepoUploadBlob: 'network.habitat.repo.uploadBlob',
} as const
