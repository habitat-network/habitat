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
  NetworkHabitatArenaGetItems: {
    lexicon: 1,
    id: 'network.habitat.arena.getItems',
    defs: {
      main: {
        type: 'query',
        description: 'Retrieve all items from a specified habitat arena.',
        permission: 'authenticated',
        parameters: {
          type: 'params',
          required: ['arenaID'],
          properties: {
            arenaID: {
              type: 'string',
              description:
                'The ID of the arena to retrieve items from, formatted as a habitat-uri.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['allowToken', 'items'],
            properties: {
              allowToken: {
                type: 'string',
                description:
                  "Token providing proof that the caller can read the record, verifiable by the repos hosting the arena's items.",
              },
              items: {
                description:
                  'The list of items present in the arena, referenced by habitat-uris.',
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.arena.getItems#record',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatArenaSendItem: {
    lexicon: 1,
    id: 'network.habitat.arena.sendItem',
    defs: {
      main: {
        type: 'procedure',
        description: 'Send an item to a specified habitat arena.',
        permission: 'authenticated',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['item', 'arenaID'],
            properties: {
              item: {
                type: 'string',
                description:
                  'The URI for the item to send to the arena, formatted as a habitat-uri.',
              },
              arenaID: {
                type: 'string',
                description:
                  'The ID of the arena to send the item to, formatted as a habitat-uri.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              status: {
                type: 'string',
                description:
                  "Result status of the send operation, e.g., 'success' or 'error'.",
              },
              message: {
                type: 'string',
                description:
                  'Optional message providing additional information about the operation.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatInternalGetRecord: {
    lexicon: 1,
    id: 'network.habitat.internal.getRecord',
    defs: {
      main: {
        type: 'query',
        permission: 'signed',
        description:
          'Get a single record from a repository, and provide the proof that the caller is allowed to do so.',
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
            allowToken: {
              type: 'string',
              description:
                'Optional token providing proof the requester can read the record, verifiable by the resource server (if the record has delegated its permissions to another DID).',
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
                description: 'The habitat-uri for this record.',
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
  NetworkHabitatInternalNotifyOfUpdate: {
    lexicon: 1,
    id: 'network.habitat.internal.notifyOfUpdate',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Notify another DID that there is an update for them on the fiven record.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['collection', 'did'],
            properties: {
              collection: {
                type: 'string',
                format: 'nsid',
                description:
                  'The NSID of the record collection that the update is for.',
              },
              did: {
                type: 'string',
                format: 'did',
                description: 'The DID to grant permission to (URL parameter).',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              status: {
                type: 'string',
                description:
                  "Result status of the permission grant, e.g., 'success' or 'error'.",
              },
              message: {
                type: 'string',
                description:
                  'Optional message providing more details about the operation.',
              },
            },
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
                description: 'The habitat-uri for this record.',
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
  NetworkHabitatListRecords: {
    lexicon: 1,
    id: 'network.habitat.listRecords',
    defs: {
      main: {
        type: 'procedure',
        description:
          'List records with optional filters for subjects, lexicons, and timestamps.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['subjects', 'collection'],
            properties: {
              subjects: {
                type: 'array',
                items: {
                  type: 'string',
                  description:
                    'Repos (DIDs) or arenas (habitat-uris) to search from to retrieve records.',
                },
              },
              collection: {
                type: 'string',
                description: 'Filter by specific lexicons',
                items: {
                  type: 'string',
                  format: 'nsid',
                },
              },
              since: {
                type: 'string',
                description:
                  'Allow getting records that are strictly newer or updated since a certain time.',
                format: 'datetime',
              },
              limit: {
                type: 'integer',
                description:
                  '[UNIMPLEMENTED] The number of records to return. (Default value should be 50 to be consistent with atproto API).',
              },
              cursor: {
                type: 'string',
                description: '[UNIMPLEMENTED] Cursor of the returned list.',
              },
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
                  ref: 'lex:network.habitat.listRecords#record',
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
            description:
              'URI reference to the record, formatted as a habitat-uri.',
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
              grantees: {
                type: 'array',
                items: {
                  type: 'string',
                  description:
                    'Grantees as either DIDs or Arena refs [TODO: make a union]',
                },
              },
              createArena: {
                type: 'boolean',
                description:
                  'Whether to create an arena, allowing all grantees to aggregate records under this arena.',
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
                description: 'The habitat-uri of the put-ed object.',
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
  NetworkHabitatArenaGetItems: 'network.habitat.arena.getItems',
  NetworkHabitatArenaSendItem: 'network.habitat.arena.sendItem',
  NetworkHabitatInternalGetRecord: 'network.habitat.internal.getRecord',
  NetworkHabitatInternalNotifyOfUpdate:
    'network.habitat.internal.notifyOfUpdate',
  NetworkHabitatPhoto: 'network.habitat.photo',
  NetworkHabitatRepoGetBlob: 'network.habitat.repo.getBlob',
  NetworkHabitatRepoGetRecord: 'network.habitat.repo.getRecord',
  NetworkHabitatListRecords: 'network.habitat.listRecords',
  NetworkHabitatRepoPutRecord: 'network.habitat.repo.putRecord',
  NetworkHabitatRepoUploadBlob: 'network.habitat.repo.uploadBlob',
} as const
