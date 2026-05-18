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
  CommunityLexiconCalendarInvite: {
    lexicon: 1,
    id: 'community.lexicon.calendar.invite',
    defs: {
      main: {
        type: 'record',
        description:
          "An invitation to a calendar event. Stored on the inviter's PDS.",
        key: 'tid',
        record: {
          type: 'object',
          required: ['subject', 'invitee', 'createdAt'],
          properties: {
            subject: {
              type: 'ref',
              ref: 'lex:com.atproto.repo.strongRef',
              description: 'Reference to the calendar event.',
            },
            invitee: {
              type: 'string',
              format: 'did',
              description: 'The DID of the person being invited.',
            },
            createdAt: {
              type: 'string',
              format: 'datetime',
              description: 'Timestamp when the invitation was created.',
            },
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
  NetworkHabitatClique: {
    lexicon: 1,
    id: 'network.habitat.clique',
    defs: {
      main: {
        type: 'object',
        description:
          'This collection is reserved for special purposes and cannot be directly written to. To read/set cliques, use clique-specific XRPC APIs.',
        properties: {},
      },
    },
  },
  NetworkHabitatCliqueAddMembers: {
    lexicon: 1,
    id: 'network.habitat.clique.addMembers',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Add member(s) to a clique. The clique must be editable by the caller.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['clique', 'members'],
            properties: {
              clique: {
                type: 'ref',
                ref: 'lex:network.habitat.grantee#clique',
              },
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatCliqueCreateClique: {
    lexicon: 1,
    id: 'network.habitat.clique.createClique',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Create a clique. The clique is owned by the caller. Optionally provide members to be added to this clique.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['clique'],
            properties: {
              clique: {
                type: 'string',
                description:
                  'The habitat clique, formatted as clique:<owner did>/<clique key>',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatCliqueGetMembers: {
    lexicon: 1,
    id: 'network.habitat.clique.getMembers',
    defs: {
      main: {
        type: 'query',
        description:
          'See the member(s) to a clique. The clique must be readable by the caller.',
        parameters: {
          type: 'params',
          required: ['clique'],
          properties: {
            clique: {
              type: 'string',
              description:
                'The desired clique to query formatted as clique:<owner did>/<clique key>',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['members'],
            properties: {
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatCliqueIsMember: {
    lexicon: 1,
    id: 'network.habitat.clique.isMember',
    defs: {
      main: {
        type: 'query',
        description:
          'See if a user belongs to a clique. The clique must be readable by the caller.',
        parameters: {
          type: 'params',
          required: ['clique', 'did'],
          properties: {
            clique: {
              type: 'string',
              description:
                'The desired clique to query formatted as clique:<owner did>/<clique key>',
            },
            did: {
              type: 'string',
              format: 'did',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['found'],
            properties: {
              found: {
                type: 'boolean',
                description:
                  'Whether the given member is a part of the given clique.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatCliqueRemoveMembers: {
    lexicon: 1,
    id: 'network.habitat.clique.removeMembers',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Remove member(s) to a clique. The clique must be editable by the caller.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['clique', 'members'],
            properties: {
              clique: {
                type: 'ref',
                ref: 'lex:network.habitat.grantee#clique',
              },
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatDocs: {
    lexicon: 1,
    id: 'network.habitat.docs',
    defs: {
      main: {
        type: 'record',
        description: 'A collaborative document.',
        key: 'tid',
        record: {
          type: 'object',
          required: ['name', 'blob'],
          properties: {
            name: {
              type: 'string',
              description:
                'The name of the document, derived from the first heading.',
            },
            blob: {
              type: 'string',
              description:
                'Base64-encoded Yjs state update representing the document content.',
            },
            editorClique: {
              type: 'string',
              format: 'uri',
              description:
                'URI of the clique whose members may edit this document.',
            },
          },
        },
      },
    },
  },
  NetworkHabitatGrantee: {
    lexicon: 1,
    id: 'network.habitat.grantee',
    defs: {
      didGrantee: {
        type: 'object',
        description: 'A DID grantee',
        required: ['did'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
        },
      },
      clique: {
        type: 'object',
        description:
          'A clique grantee in the form clique:did:plc:web:arushi/clique-key',
        required: ['clique'],
        properties: {
          clique: {
            type: 'string',
          },
        },
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
            required: ['collection', 'rkey', 'recipient'],
            properties: {
              recipient: {
                type: 'string',
                format: 'did',
                description: 'The DID to grant permission to (URL parameter).',
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description:
                  'The NSID of the record collection that the update is for.',
              },
              rkey: {
                type: 'string',
                description: 'The record key which was updated.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatListConnectedApps: {
    lexicon: 1,
    id: 'network.habitat.listConnectedApps',
    defs: {
      main: {
        type: 'query',
        description:
          'List apps connected to habitat for a given user. Returns connected apps for the authenticated user.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['apps'],
            properties: {
              apps: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.listConnectedApps#app',
                },
              },
            },
          },
        },
      },
      app: {
        type: 'object',
        required: ['name', 'clientID', 'clientUri', 'lastUsed'],
        properties: {
          name: {
            type: 'string',
            description: 'The name of this app.',
          },
          clientID: {
            type: 'string',
            description: 'The ID of this app.',
          },
          clientUri: {
            type: 'string',
            description: 'The uri of this app.',
          },
          lastUsed: {
            type: 'string',
            format: 'datetime',
            description:
              'The last time habitat detected a session with this app.',
          },
          logoUri: {
            type: 'string',
            description: 'The logo URI of this app.',
          },
        },
      },
    },
  },
  NetworkHabitatOrgAddAdmin: {
    lexicon: 1,
    id: 'network.habitat.org.addAdmin',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Add an admin to the org. Only callable by existing admins.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['admin'],
            properties: {
              admin: {
                type: 'string',
                format: 'did',
                description: 'The DID of the user to add as an admin.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgAddMembers: {
    lexicon: 1,
    id: 'network.habitat.org.addMembers',
    defs: {
      main: {
        type: 'procedure',
        description: 'Add member(s) to the org. Only callable by admins.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['members'],
            properties: {
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
                description: 'The DIDs of the users to add as members.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgCreate: {
    lexicon: 1,
    id: 'network.habitat.org.create',
    defs: {
      main: {
        type: 'procedure',
        description: 'Create a new org with a bootstrap admin member.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['admin_handle', 'admin_password', 'handle_subdomain'],
            properties: {
              admin_handle: {
                type: 'string',
                description:
                  'Internal handle for the bootstrap admin (alphanumeric, 1-50 chars).',
              },
              admin_password: {
                type: 'string',
                description:
                  'Password for the bootstrap admin account (required for password login method).',
              },
              handle_subdomain: {
                type: 'string',
                description:
                  "Subdomain for all org member handles (e.g. 'acmecorp').",
              },
              name: {
                type: 'string',
                description: 'A display name for this org.',
              },
              login_method: {
                type: 'string',
                default: 'password',
                description:
                  "Login method for the org: 'password', 'atproto', or 'google'.",
              },
              login_id: {
                type: 'string',
                description:
                  "Provider-specific identifier (public ATProto DID for 'atproto', email for 'google'). Ignored for 'password'.",
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['org_id', 'admin_did', 'admin_handle', 'name'],
            properties: {
              org_id: {
                type: 'string',
                description: 'The ID of the created org.',
              },
              admin_did: {
                type: 'string',
                description: 'The DID of the bootstrap admin.',
              },
              admin_handle: {
                type: 'string',
                description: 'The full handle of the bootstrap admin.',
              },
              name: {
                type: 'string',
                description: 'The display name of the created org.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgDowngradeAdmin: {
    lexicon: 1,
    id: 'network.habitat.org.downgradeAdmin',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Downgrade an admin to a regular member. Only callable by existing admins. The last admin cannot be downgraded.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['admin'],
            properties: {
              admin: {
                type: 'string',
                format: 'did',
                description: 'The DID of the admin to downgrade to member.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgGetAdmins: {
    lexicon: 1,
    id: 'network.habitat.org.getAdmins',
    defs: {
      main: {
        type: 'query',
        description:
          'Get the list of admins in the org. Callable by any org member.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['admins'],
            properties: {
              admins: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgGetMembers: {
    lexicon: 1,
    id: 'network.habitat.org.getMembers',
    defs: {
      main: {
        type: 'query',
        description:
          'Get the list of members in the org. Callable by any org member.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['members'],
            properties: {
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgGetMetadata: {
    lexicon: 1,
    id: 'network.habitat.org.getMetadata',
    defs: {
      main: {
        type: 'query',
        description: 'Get general info about this organization.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['domain'],
            properties: {
              domain: {
                type: 'string',
                description:
                  'The domain where habitat is hosted for this organization.',
              },
              name: {
                type: 'string',
                description: 'The name of this organization.',
              },
              description: {
                type: 'string',
                description: 'A description for this organization.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgIssueInviteToken: {
    lexicon: 1,
    id: 'network.habitat.org.issueInviteToken',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Generate an invite token that can be sent to a member to join this organization.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              expiresAt: {
                type: 'string',
                format: 'datetime',
                description: 'When this token expires; defaults to 1 week.',
              },
              reusable: {
                type: 'boolean',
                description:
                  'Whether this token is reusable to invite more than one member; defaults to false.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['token'],
            properties: {
              token: {
                type: 'string',
                description: 'The generated invite token.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgLoginMember: {
    lexicon: 1,
    id: 'network.habitat.org.loginMember',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Authenticate a habitat org member with their handle and password, returning a short-lived token for use in the OAuth callback flow.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['handle', 'password'],
            properties: {
              handle: {
                type: 'string',
                description:
                  'The full handle of the member (e.g. alice.example.com).',
              },
              password: {
                type: 'string',
                description: "The member's password.",
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['callbackURL'],
            properties: {
              callbackURL: {
                type: 'string',
                description:
                  'The URL to redirect the browser to in order to complete the OAuth flow.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgMintMemberIdentity: {
    lexicon: 1,
    id: 'network.habitat.org.mintMemberIdentity',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Mint a new organization member identity with the given handle and token.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['token', 'handle', 'password'],
            properties: {
              orgId: {
                type: 'string',
                description: 'The ID of the org this member is joining.',
              },
              handle: {
                type: 'string',
                description:
                  'The internal handle (all letters + numbers, no special characters, does not include org domain) that will be used by the member.',
              },
              token: {
                type: 'string',
                description:
                  'The token that was issued by an org admin to allow members to join the organization..',
              },
              password: {
                type: 'string',
                description: "The password for the new member's account.",
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['handle', 'did'],
            properties: {
              handle: {
                type: 'string',
                description:
                  'The full handle of the newly minted member identity.',
              },
              did: {
                type: 'string',
                description: 'The DID of the newly minted member identity.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgRemoveAdmin: {
    lexicon: 1,
    id: 'network.habitat.org.removeAdmin',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Remove an admin from the org. Only callable by existing admins. The last admin cannot be removed.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['admin'],
            properties: {
              admin: {
                type: 'string',
                format: 'did',
                description: 'The DID of the admin to remove.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatOrgRemoveMembers: {
    lexicon: 1,
    id: 'network.habitat.org.removeMembers',
    defs: {
      main: {
        type: 'procedure',
        description: 'Remove member(s) from the org. Only callable by admins.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['members'],
            properties: {
              members: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
                description: 'The DIDs of the members to remove.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatPermissionsAddPermission: {
    lexicon: 1,
    id: 'network.habitat.permissions.addPermission',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Grant read permission to a user or a clique for a specific collection or a specific record.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['grantees', 'collection'],
            properties: {
              grantees: {
                type: 'array',
                items: {
                  type: 'union',
                  refs: [
                    'lex:network.habitat.grantee#didGrantee',
                    'lex:network.habitat.grantee#clique',
                  ],
                },
                maxLength: 100,
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description:
                  'The NSID of the lexicon or record to grant read permission for.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description:
                  'The Record Key to grant read permissions to, if any.',
                maxLength: 512,
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatPermissionsListPermissions: {
    lexicon: 1,
    id: 'network.habitat.permissions.listPermissions',
    defs: {
      permission: {
        type: 'object',
        required: ['grantee', 'collection', 'effect'],
        properties: {
          grantee: {
            type: 'string',
            description:
              'The grantee of the permission — either a DID or a habitat clique URI.',
          },
          collection: {
            type: 'string',
            format: 'nsid',
            description:
              'The NSID of the collection the permission applies to.',
          },
          rkey: {
            type: 'string',
            description:
              'The record key the permission applies to. Empty string means the permission covers the entire collection.',
          },
          effect: {
            type: 'string',
            knownValues: ['allow', 'deny'],
            description: 'Whether this permission grants or denies access.',
          },
        },
      },
      main: {
        type: 'query',
        description: 'List read permissions visible to the authenticated user.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['permissions'],
            properties: {
              permissions: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.permissions.listPermissions#permission',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatPermissionsRemovePermission: {
    lexicon: 1,
    id: 'network.habitat.permissions.removePermission',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Revoke read permission from a user or a clique for a specific lexicon or record.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['grantees', 'collection'],
            properties: {
              grantees: {
                type: 'array',
                items: {
                  type: 'union',
                  refs: [
                    'lex:network.habitat.grantee#didGrantee',
                    'lex:network.habitat.grantee#clique',
                  ],
                },
                maxLength: 100,
              },
              collection: {
                type: 'string',
                format: 'nsid',
                description:
                  'The NSID of the lexicon or record to grant read permission for.',
              },
              rkey: {
                type: 'string',
                format: 'record-key',
                description:
                  'The Record Key to grant read permissions to, if any.',
                maxLength: 512,
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
  NetworkHabitatRenderSchema: {
    lexicon: 1,
    id: 'network.habitat.render.schema',
    defs: {
      main: {
        type: 'record',
        description:
          'A render schema describing how to display records of a given lexicon type.',
        key: 'any',
        record: {
          type: 'object',
          required: ['targetLexicon', 'title', 'fields'],
          properties: {
            targetLexicon: {
              type: 'string',
              description:
                'The NSID of the lexicon this render schema applies to.',
            },
            title: {
              type: 'string',
              description: 'Human-readable name for this record type.',
            },
            description: {
              type: 'string',
              description:
                'A brief description of what this record type represents.',
            },
            fields: {
              type: 'array',
              description: 'Ordered list of field display descriptors.',
              items: {
                type: 'ref',
                ref: 'lex:network.habitat.render.schema#fieldSchema',
              },
            },
          },
        },
      },
      fieldSchema: {
        type: 'object',
        description: 'Describes how to display a single field of a record.',
        required: ['path', 'label', 'displayType', 'priority'],
        properties: {
          path: {
            type: 'string',
            description:
              "Dot-notation path into the record value (e.g. 'name', 'startsAt').",
          },
          label: {
            type: 'string',
            description: 'Human-readable label for this field.',
          },
          displayType: {
            type: 'string',
            description: 'How to render the value.',
            knownValues: [
              'network.habitat.render.schema#text',
              'network.habitat.render.schema#datetime',
              'network.habitat.render.schema#url',
              'network.habitat.render.schema#badge',
              'network.habitat.render.schema#list',
            ],
          },
          priority: {
            type: 'string',
            description: 'Layout prominence of this field.',
            knownValues: [
              'network.habitat.render.schema#primary',
              'network.habitat.render.schema#secondary',
              'network.habitat.render.schema#metadata',
            ],
          },
          optional: {
            type: 'boolean',
            description:
              'If true, omit this field from display when its value is missing or empty.',
            default: false,
          },
        },
      },
      text: {
        type: 'token',
        description: 'Render as plain text.',
      },
      datetime: {
        type: 'token',
        description: 'Render as a formatted date/time string.',
      },
      url: {
        type: 'token',
        description: 'Render as a hyperlink.',
      },
      badge: {
        type: 'token',
        description:
          'Render as a pill badge, extracting the token name from an NSID#token value.',
      },
      list: {
        type: 'token',
        description: 'Render as a list of items.',
      },
      primary: {
        type: 'token',
        description:
          "Most prominent display — used for the record's main identifier (e.g. title).",
      },
      secondary: {
        type: 'token',
        description: 'Standard field-value display.',
      },
      metadata: {
        type: 'token',
        description: 'De-emphasized display, shown at the bottom or collapsed.',
      },
    },
  },
  NetworkHabitatRepoCreateRecord: {
    lexicon: 1,
    id: 'network.habitat.repo.createRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Create a pear repository record, creating it if it does not exist already, or updating it as needed.',
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
                description: 'The record to write.',
              },
              createGranteesClique: {
                type: 'boolean',
                description:
                  'Whether to create a clique with the given grantees. If true, all grantees must be DIDs, and the created clique ref is returned.',
              },
              grantees: {
                type: 'array',
                description: 'Any grantees to set for this record',
                items: {
                  type: 'union',
                  refs: [
                    'lex:network.habitat.grantee#didGrantee',
                    'lex:network.habitat.grantee#clique',
                  ],
                },
                maxLength: 100,
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
                format: 'uri',
                description: 'The habitat-uri of the put-ed object.',
              },
              validationStatus: {
                type: 'string',
                knownValues: ['valid', 'unknown'],
              },
              clique: {
                type: 'string',
                description:
                  'If a clique was created, return its ref, formatted like clique:<owner did>/<clique key>',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatRepoDeleteRecord: {
    lexicon: 1,
    id: 'network.habitat.repo.deleteRecord',
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
            },
          },
        },
      },
    },
  },
  NetworkHabitatRepoDescribeRepo: {
    lexicon: 1,
    id: 'network.habitat.repo.describeRepo',
    defs: {
      main: {
        type: 'query',
        description:
          'Get information about an account and repository, including the list of collections.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: [
              'handle',
              'did',
              'didDoc',
              'collections',
              'handleIsCorrect',
            ],
            properties: {
              handle: {
                type: 'string',
                format: 'handle',
              },
              did: {
                type: 'string',
                format: 'did',
              },
              didDoc: {
                type: 'unknown',
              },
              collections: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.repo.describeRepo#collectionMetadata',
                },
              },
              handleIsCorrect: {
                type: 'boolean',
              },
            },
          },
        },
      },
      collectionMetadata: {
        type: 'object',
        required: ['nsid', 'recordCount', 'lastTouched', 'grantees'],
        properties: {
          nsid: {
            type: 'string',
            description: 'The NSID of this collection.',
          },
          recordCount: {
            type: 'integer',
            description: 'Number of records for this collection.',
          },
          lastTouched: {
            type: 'string',
            format: 'datetime',
            description:
              'The last time a record in this collection was touched.',
          },
          grantees: {
            type: 'array',
            items: {
              type: 'union',
              refs: [
                'lex:network.habitat.grantee#didGrantee',
                'lex:network.habitat.grantee#clique',
              ],
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
        description: 'Get a single record from the pear repository.',
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
            includePermissions: {
              type: 'boolean',
              description:
                'Whether to return the permission grants on this record as part of the response.',
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
                format: 'uri',
                description: 'The habitat-uri for this record.',
              },
              value: {
                type: 'unknown',
              },
              permissions: {
                type: 'array',
                items: {
                  type: 'union',
                  refs: [
                    'lex:network.habitat.grantee#didGrantee',
                    'lex:network.habitat.grantee#clique',
                  ],
                },
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
          'List records with optional filters for subjects, lexicons, and timestamps.',
        parameters: {
          type: 'params',
          required: ['subjects', 'collection'],
          properties: {
            subjects: {
              type: 'array',
              items: {
                type: 'string',
                description: 'Repos (DIDs) to search from to retrieve records.',
              },
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'Filter by specific lexicon.',
            },
            includePermissions: {
              type: 'boolean',
              description:
                'Whether to return the permission grants on this record as part of the response.',
            },
            since: {
              type: 'string',
              description:
                ' [UNIMPLEMENTED] Allow getting records that are strictly newer or updated since a certain time.',
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
        required: ['uri', 'value'],
        properties: {
          uri: {
            type: 'string',
            format: 'uri',
            description:
              'URI reference to the record, formatted as a habitat-uri.',
          },
          value: {
            type: 'unknown',
          },
          permissions: {
            type: 'array',
            items: {
              type: 'union',
              refs: [
                'lex:network.habitat.grantee#didGrantee',
                'lex:network.habitat.grantee#clique',
              ],
            },
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
          'Write a pear repository record, creating it if it does not exist already, or updating it as needed.',
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
                description: 'The record to write.',
              },
              grantees: {
                type: 'array',
                items: {
                  type: 'union',
                  refs: [
                    'lex:network.habitat.grantee#didGrantee',
                    'lex:network.habitat.grantee#clique',
                  ],
                },
                maxLength: 100,
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
                format: 'uri',
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
  CommunityLexiconCalendarInvite: 'community.lexicon.calendar.invite',
  CommunityLexiconCalendarRsvp: 'community.lexicon.calendar.rsvp',
  CommunityLexiconLocationAddress: 'community.lexicon.location.address',
  CommunityLexiconLocationFsq: 'community.lexicon.location.fsq',
  CommunityLexiconLocationGeo: 'community.lexicon.location.geo',
  CommunityLexiconLocationHthree: 'community.lexicon.location.hthree',
  NetworkHabitatClique: 'network.habitat.clique',
  NetworkHabitatCliqueAddMembers: 'network.habitat.clique.addMembers',
  NetworkHabitatCliqueCreateClique: 'network.habitat.clique.createClique',
  NetworkHabitatCliqueGetMembers: 'network.habitat.clique.getMembers',
  NetworkHabitatCliqueIsMember: 'network.habitat.clique.isMember',
  NetworkHabitatCliqueRemoveMembers: 'network.habitat.clique.removeMembers',
  NetworkHabitatDocs: 'network.habitat.docs',
  NetworkHabitatGrantee: 'network.habitat.grantee',
  NetworkHabitatInternalNotifyOfUpdate:
    'network.habitat.internal.notifyOfUpdate',
  NetworkHabitatListConnectedApps: 'network.habitat.listConnectedApps',
  NetworkHabitatOrgAddAdmin: 'network.habitat.org.addAdmin',
  NetworkHabitatOrgAddMembers: 'network.habitat.org.addMembers',
  NetworkHabitatOrgCreate: 'network.habitat.org.create',
  NetworkHabitatOrgDowngradeAdmin: 'network.habitat.org.downgradeAdmin',
  NetworkHabitatOrgGetAdmins: 'network.habitat.org.getAdmins',
  NetworkHabitatOrgGetMembers: 'network.habitat.org.getMembers',
  NetworkHabitatOrgGetMetadata: 'network.habitat.org.getMetadata',
  NetworkHabitatOrgIssueInviteToken: 'network.habitat.org.issueInviteToken',
  NetworkHabitatOrgLoginMember: 'network.habitat.org.loginMember',
  NetworkHabitatOrgMintMemberIdentity: 'network.habitat.org.mintMemberIdentity',
  NetworkHabitatOrgRemoveAdmin: 'network.habitat.org.removeAdmin',
  NetworkHabitatOrgRemoveMembers: 'network.habitat.org.removeMembers',
  NetworkHabitatPermissionsAddPermission:
    'network.habitat.permissions.addPermission',
  NetworkHabitatPermissionsListPermissions:
    'network.habitat.permissions.listPermissions',
  NetworkHabitatPermissionsRemovePermission:
    'network.habitat.permissions.removePermission',
  NetworkHabitatPhoto: 'network.habitat.photo',
  NetworkHabitatRenderSchema: 'network.habitat.render.schema',
  NetworkHabitatRepoCreateRecord: 'network.habitat.repo.createRecord',
  NetworkHabitatRepoDeleteRecord: 'network.habitat.repo.deleteRecord',
  NetworkHabitatRepoDescribeRepo: 'network.habitat.repo.describeRepo',
  NetworkHabitatRepoGetBlob: 'network.habitat.repo.getBlob',
  NetworkHabitatRepoGetRecord: 'network.habitat.repo.getRecord',
  NetworkHabitatRepoListRecords: 'network.habitat.repo.listRecords',
  NetworkHabitatRepoPutRecord: 'network.habitat.repo.putRecord',
  NetworkHabitatRepoUploadBlob: 'network.habitat.repo.uploadBlob',
} as const
