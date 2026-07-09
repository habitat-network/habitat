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
  ComAtprotoRepoDescribeRepo: {
    lexicon: 1,
    id: 'com.atproto.repo.describeRepo',
    defs: {
      main: {
        type: 'query',
        description:
          'Get information about an account and repository, including the list of collections. Does not require auth.',
        parameters: {
          type: 'params',
          required: ['repo'],
          properties: {
            repo: {
              type: 'string',
              format: 'at-identifier',
              description: 'The handle or DID of the repo.',
            },
          },
        },
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
                description: 'The complete DID document for this account.',
              },
              collections: {
                type: 'array',
                description:
                  'List of all the collections (NSIDs) for which this repo contains at least one record.',
                items: {
                  type: 'string',
                  format: 'nsid',
                },
              },
              handleIsCorrect: {
                type: 'boolean',
                description:
                  'Indicates if handle is currently valid (resolves bi-directionally)',
              },
            },
          },
        },
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
  ComAtprotoServerGetServiceAuth: {
    lexicon: 1,
    id: 'com.atproto.server.getServiceAuth',
    defs: {
      main: {
        type: 'query',
        description:
          'Get a signed token on behalf of the requesting DID for the requested service.',
        parameters: {
          type: 'params',
          required: ['aud'],
          properties: {
            aud: {
              type: 'string',
              format: 'did',
              description:
                'The DID of the service that the token will be used to authenticate with',
            },
            exp: {
              type: 'integer',
              description:
                'The time in Unix Epoch seconds that the JWT expires. Defaults to 60 seconds in the future. The service may enforce certain time bounds on tokens depending on the requested scope.',
            },
            lxm: {
              type: 'string',
              format: 'nsid',
              description:
                'Lexicon (XRPC) method to bind the requested token to',
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
              },
            },
          },
        },
        errors: [
          {
            name: 'BadExpiration',
            description:
              'Indicates that the requested expiration date is not a valid. May be in the past or may be reliant on the requested scopes.',
          },
        ],
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
  NetworkHabitatAdminGetSettings: {
    lexicon: 1,
    id: 'network.habitat.admin.getSettings',
    defs: {
      main: {
        type: 'query',
        description:
          "Get this instance's admin-configurable settings. Requires an authenticated instance admin session.",
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['instanceName', 'orgCreationPolicy'],
            properties: {
              instanceName: {
                type: 'string',
                description: "This instance's display name.",
              },
              orgCreationPolicy: {
                type: 'string',
                description: "'open' or 'invite_only'.",
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatAdminIssueInvite: {
    lexicon: 1,
    id: 'network.habitat.admin.issueInvite',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Issue a single-use invite token for creating an org on this instance. Requires an authenticated instance admin session.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['token'],
            properties: {
              token: {
                type: 'string',
                description:
                  'Signed, single-use invite token to embed in an org-creation link.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatAdminUpdateSettings: {
    lexicon: 1,
    id: 'network.habitat.admin.updateSettings',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Update this instance's admin-configurable settings. Requires an authenticated instance admin session.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              instanceName: {
                type: 'string',
                description:
                  "This instance's display name. Omit to leave unchanged.",
              },
              orgCreationPolicy: {
                type: 'string',
                description:
                  "'open' or 'invite_only'. Omit to leave unchanged.",
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['instanceName', 'orgCreationPolicy'],
            properties: {
              instanceName: {
                type: 'string',
              },
              orgCreationPolicy: {
                type: 'string',
              },
            },
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
  NetworkHabitatCollectionsDefs: {
    lexicon: 1,
    id: 'network.habitat.collections.defs',
    defs: {
      collectionView: {
        type: 'object',
        description:
          "A record collection (lexicon type) present in the org's synced data, with a count of the records in it the calling user can see. A record scoped to more than one readable space is counted once per space, since each space holds its own version.",
        required: ['collection', 'recordCount'],
        properties: {
          collection: {
            type: 'string',
            format: 'nsid',
            description: 'The NSID of the record collection.',
          },
          recordCount: {
            type: 'integer',
            description:
              'Number of records in this collection the calling user can see, counted across all spaces they can read (once per space a record belongs to).',
          },
        },
      },
      recordView: {
        type: 'object',
        description:
          'A single record scoped to one space. The same repo/collection/rkey in a different space is a distinct record with its own version, so it appears as its own recordView. The record body is not included; fetch it on demand from pear using the space, repo, collection and rkey.',
        required: ['uri', 'space', 'repo', 'collection', 'rkey'],
        properties: {
          uri: {
            type: 'string',
            format: 'uri',
            description:
              'The space-record URI (spaceUri/repo/collection/rkey), unique to this record in this space.',
          },
          space: {
            type: 'string',
            format: 'uri',
            description: 'URI of the space this record belongs to.',
          },
          repo: {
            type: 'string',
            format: 'did',
            description: 'DID of the repo the record lives in.',
          },
          collection: {
            type: 'string',
            format: 'nsid',
            description: 'The NSID of the record collection.',
          },
          rkey: {
            type: 'string',
            format: 'record-key',
            description: 'The record key.',
          },
        },
      },
    },
  },
  NetworkHabitatCollectionsListCollections: {
    lexicon: 1,
    id: 'network.habitat.collections.listCollections',
    defs: {
      main: {
        type: 'query',
        description:
          "List the record collections (lexicon types) present in the org's synced data, each with a count of the distinct records the calling user can see. Only collections with at least one visible record are returned. Implemented by the home server and reached via pear service proxying.",
        parameters: {
          type: 'params',
          properties: {},
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['collections'],
            properties: {
              collections: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.collections.defs#collectionView',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatCollectionsListRecords: {
    lexicon: 1,
    id: 'network.habitat.collections.listRecords',
    defs: {
      main: {
        type: 'query',
        description:
          'List the records in a collection the calling user can see, each with the spaces it belongs to that the user can read. The record body is not included; fetch it on demand from pear. Implemented by the home server and reached via pear service proxying.',
        parameters: {
          type: 'params',
          required: ['collection'],
          properties: {
            collection: {
              type: 'string',
              format: 'nsid',
              description: 'The NSID of the record collection to list.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['records'],
            properties: {
              records: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.collections.defs#recordView',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatDocsCrdt: {
    lexicon: 1,
    id: 'network.habitat.docs.crdt',
    defs: {
      main: {
        type: 'record',
        description:
          "The CRDT (Yjs) state of a collaborative document. Each document is its own space; this record holds the canonical document state under the literal key 'self'.",
        key: 'literal:self',
        record: {
          type: 'object',
          required: ['blob'],
          properties: {
            blob: {
              type: 'string',
              description:
                'Base64-encoded Yjs state update representing the document content.',
            },
          },
        },
      },
    },
  },
  NetworkHabitatDocsCreateDoc: {
    lexicon: 1,
    id: 'network.habitat.docs.createDoc',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Create a new collaborative document. Implemented by the docs server, which writes the canonical record into the org's docs space using the org credential.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {},
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri', 'docId'],
            properties: {
              uri: {
                type: 'string',
                description: 'URI of the created document record.',
              },
              docId: {
                type: 'string',
                description:
                  'The record key identifying the document, used in subsequent updateDoc calls.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatDocsListDocs: {
    lexicon: 1,
    id: 'network.habitat.docs.listDocs',
    defs: {
      main: {
        type: 'query',
        description:
          "List all documents in the org, with titles. Implemented by the docs server, which lists the doc spaces from pear using the org credential and reads each space's markdown 'self' record for the title.",
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['docs'],
            properties: {
              docs: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.docs.listDocs#docView',
                },
              },
            },
          },
        },
      },
      docView: {
        type: 'object',
        required: ['docId', 'uri', 'title'],
        properties: {
          docId: {
            type: 'string',
            description:
              "The doc's space key, used as the document identifier in updateDoc and routing.",
          },
          uri: {
            type: 'string',
            description: "URI of the doc's space.",
          },
          title: {
            type: 'string',
            description: "The document title, from its markdown 'self' record.",
          },
        },
      },
    },
  },
  NetworkHabitatDocsMarkdown: {
    lexicon: 1,
    id: 'network.habitat.docs.markdown',
    defs: {
      main: {
        type: 'record',
        description:
          "The rendered markdown of a collaborative document, derived from its CRDT state by the docs server. One per doc space, under the literal key 'self'.",
        key: 'literal:self',
        record: {
          type: 'object',
          required: ['title', 'content'],
          properties: {
            title: {
              type: 'string',
              description:
                'The document title, derived from the first heading or line.',
            },
            content: {
              type: 'string',
              description: 'The rendered markdown content of the document.',
            },
          },
        },
      },
    },
  },
  NetworkHabitatDocsUpdateDoc: {
    lexicon: 1,
    id: 'network.habitat.docs.updateDoc',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Apply a CRDT update to a collaborative document. Implemented by the docs server, which merges the update into the canonical document and writes it back using the org credential.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['docId', 'update'],
            properties: {
              docId: {
                type: 'string',
                description:
                  'The record key identifying the document to update.',
              },
              update: {
                type: 'string',
                description:
                  'Base64-encoded Yjs update to merge into the document.',
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
                description: 'URI of the updated document record.',
              },
              cid: {
                type: 'string',
                format: 'cid',
                description: 'CID of the updated record.',
              },
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
  NetworkHabitatGroupProfile: {
    lexicon: 1,
    id: 'network.habitat.group.profile',
    defs: {
      main: {
        type: 'record',
        description:
          "Metadata for a group. A group is a space of type `network.habitat.group`; this profile record is the group-space's metadata record, holding its display name and description. Group membership is expressed as roles on the group-space (at least writer role implies membership), and the group can be used as a grantee elsewhere via a network.habitat.relationship.defs#spaceRoleSubject that references the group-space with role 'writer'.",
        key: 'literal:self',
        record: {
          type: 'object',
          required: ['name'],
          properties: {
            name: {
              type: 'string',
              maxLength: 256,
            },
            description: {
              type: 'string',
              maxLength: 2048,
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
  NetworkHabitatGroupsAddMember: {
    lexicon: 1,
    id: 'network.habitat.groups.addMember',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Add a member to a group. The member is either an individual user (subjectDid) or another group whose members are inherited (subjectGroup). The home server writes the backing relationship tuple using the org credential. Caller must be able to manage the group.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['group'],
            properties: {
              group: {
                type: 'string',
                format: 'uri',
                description: 'URI of the group-space to add the member to.',
              },
              subjectDid: {
                type: 'string',
                format: 'did',
                description:
                  'DID of the user to add as a member. Mutually exclusive with subjectGroup.',
              },
              subjectGroup: {
                type: 'string',
                format: 'uri',
                description:
                  'URI of another group-space whose members this group should inherit. Mutually exclusive with subjectDid.',
              },
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            properties: {
              uri: {
                type: 'string',
                description: 'URI of the written relationship tuple.',
              },
            },
          },
        },
        errors: [
          {
            name: 'GroupNotFound',
            description: 'No group with the given URI is indexed.',
          },
          {
            name: 'Forbidden',
            description: 'The caller is not allowed to manage this group.',
          },
          {
            name: 'InvalidSubject',
            description:
              'Exactly one of subjectDid or subjectGroup must be provided.',
          },
        ],
      },
    },
  },
  NetworkHabitatGroupsCreateGroup: {
    lexicon: 1,
    id: 'network.habitat.groups.createGroup',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Create a new group. The home server creates a network.habitat.group space using the org credential, writes a network.habitat.group.profile self record, and grants the calling user the manager role so they are both a member and able to manage the group.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['name'],
            properties: {
              name: {
                type: 'string',
                maxLength: 256,
              },
              description: {
                type: 'string',
                maxLength: 2048,
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
                description: 'URI of the created group-space.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatGroupsDefs: {
    lexicon: 1,
    id: 'network.habitat.groups.defs',
    defs: {
      groupView: {
        type: 'object',
        description:
          'A group, backed by a network.habitat.group space, with its membership resolved. Membership is the set of users holding at least the writer role on the group-space, expanded through inherited groups.',
        required: ['uri', 'name', 'isMember', 'canManage'],
        properties: {
          uri: {
            type: 'string',
            format: 'uri',
            description: 'URI of the group-space.',
          },
          name: {
            type: 'string',
          },
          description: {
            type: 'string',
          },
          createdAt: {
            type: 'string',
            format: 'datetime',
          },
          memberCount: {
            type: 'integer',
            description:
              'Number of distinct members after expanding inherited groups.',
          },
          isMember: {
            type: 'boolean',
            description: 'Whether the calling user is a member of this group.',
          },
          canManage: {
            type: 'boolean',
            description:
              'Whether the calling user can manage this group (add members, edit it).',
          },
          members: {
            type: 'array',
            items: {
              type: 'ref',
              ref: 'lex:network.habitat.groups.defs#memberView',
            },
          },
          inheritedGroups: {
            type: 'array',
            description: 'Other groups this group inherits members from.',
            items: {
              type: 'ref',
              ref: 'lex:network.habitat.groups.defs#groupRef',
            },
          },
        },
      },
      memberView: {
        type: 'object',
        required: ['did', 'direct'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
          role: {
            type: 'string',
            description:
              'Role held on the group-space (owner|manager|writer|reader).',
          },
          direct: {
            type: 'boolean',
            description:
              'True if the member is granted a role directly on this group, false if the membership is inherited from another group.',
          },
          viaGroup: {
            type: 'string',
            format: 'uri',
            description:
              'If inherited, the URI of the group-space the membership came from.',
          },
        },
      },
      groupRef: {
        type: 'object',
        required: ['uri', 'name'],
        properties: {
          uri: {
            type: 'string',
            format: 'uri',
          },
          name: {
            type: 'string',
          },
        },
      },
    },
  },
  NetworkHabitatGroupsDeleteMember: {
    lexicon: 1,
    id: 'network.habitat.groups.deleteMember',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Remove a member from a group. The member is either an individual user (subjectDid) or an inherited group (subjectGroup). The home server deletes the backing relationship tuple using the org credential. Caller must be able to manage the group.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['group'],
            properties: {
              group: {
                type: 'string',
                format: 'uri',
                description:
                  'URI of the group-space to remove the member from.',
              },
              subjectDid: {
                type: 'string',
                format: 'did',
                description:
                  'DID of the user to remove. Mutually exclusive with subjectGroup.',
              },
              subjectGroup: {
                type: 'string',
                format: 'uri',
                description:
                  'URI of an inherited group-space to stop inheriting. Mutually exclusive with subjectDid.',
              },
            },
          },
        },
        errors: [
          {
            name: 'GroupNotFound',
            description: 'No group with the given URI is indexed.',
          },
          {
            name: 'Forbidden',
            description: 'The caller is not allowed to manage this group.',
          },
          {
            name: 'InvalidSubject',
            description:
              'Exactly one of subjectDid or subjectGroup must be provided.',
          },
          {
            name: 'MemberNotFound',
            description: 'The subject is not a direct member of the group.',
          },
        ],
      },
    },
  },
  NetworkHabitatGroupsGetGroup: {
    lexicon: 1,
    id: 'network.habitat.groups.getGroup',
    defs: {
      main: {
        type: 'query',
        description:
          'Fetch a single group with its full membership expanded, including which other groups it inherits members from. Implemented by the home server and reached via pear service proxying.',
        parameters: {
          type: 'params',
          required: ['group'],
          properties: {
            group: {
              type: 'string',
              format: 'uri',
              description: 'URI of the group-space.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'ref',
            ref: 'lex:network.habitat.groups.defs#groupView',
          },
        },
        errors: [
          {
            name: 'GroupNotFound',
            description: 'No group with the given URI is indexed.',
          },
        ],
      },
    },
  },
  NetworkHabitatGroupsListGroups: {
    lexicon: 1,
    id: 'network.habitat.groups.listGroups',
    defs: {
      main: {
        type: 'query',
        description:
          'List the groups visible to the calling user: groups they are a member of (directly or through inherited groups) and groups they can manage. Implemented by the home server and reached via pear service proxying.',
        parameters: {
          type: 'params',
          properties: {},
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['groups'],
            properties: {
              groups: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.groups.defs#groupView',
                },
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatGroupsUpdateGroup: {
    lexicon: 1,
    id: 'network.habitat.groups.updateGroup',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Update a group's profile (name and/or description). Caller must be able to manage the group.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['group'],
            properties: {
              group: {
                type: 'string',
                format: 'uri',
                description: 'URI of the group-space.',
              },
              name: {
                type: 'string',
                maxLength: 256,
              },
              description: {
                type: 'string',
                maxLength: 2048,
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
              },
            },
          },
        },
        errors: [
          {
            name: 'GroupNotFound',
            description: 'No group with the given URI is indexed.',
          },
          {
            name: 'Forbidden',
            description: 'The caller is not allowed to manage this group.',
          },
        ],
      },
    },
  },
  NetworkHabitatInstanceDescribeInstance: {
    lexicon: 1,
    id: 'network.habitat.instance.describeInstance',
    defs: {
      main: {
        type: 'query',
        description:
          'Get public info about this instance. Modeled on com.atproto.server.describeServer. No authentication required.',
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['name', 'inviteRequired'],
            properties: {
              name: {
                type: 'string',
                description: "This instance's manager-configured display name.",
              },
              inviteRequired: {
                type: 'boolean',
                description:
                  'Whether creating an org on this instance requires an invite token.',
              },
            },
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
            required: ['admin_handle', 'contact_email'],
            properties: {
              admin_handle: {
                type: 'string',
                description:
                  'Internal handle for the bootstrap admin (alphanumeric, 1-50 chars).',
              },
              contact_email: {
                type: 'string',
                description:
                  'Email address for contacting the org about its account (not used for login).',
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
              invite_token: {
                type: 'string',
                description:
                  "Single-use invite token from an instance admin, required when the instance's org creation policy is invite_only.",
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
                  type: 'ref',
                  ref: 'lex:network.habitat.org.getAdmins#member',
                },
              },
            },
          },
        },
      },
      member: {
        type: 'object',
        required: ['did', 'handle'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
          handle: {
            type: 'string',
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
                  type: 'ref',
                  ref: 'lex:network.habitat.org.getMembers#member',
                },
              },
            },
          },
        },
      },
      member: {
        type: 'object',
        required: ['did', 'handle'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
          handle: {
            type: 'string',
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
        parameters: {
          type: 'params',
          properties: {
            orgId: {
              type: 'string',
              description:
                "The orge ID of the organization to look up. If not specified, defaults to the authenticated caller's org.",
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['loginMethod', 'handleSubdomain', 'orgId'],
            properties: {
              name: {
                type: 'string',
                description: 'The name of this organization.',
              },
              description: {
                type: 'string',
                description: 'A description for this organization.',
              },
              loginMethod: {
                type: 'string',
                description:
                  "Login method for the org: 'password', 'atproto', or 'google'.",
              },
              handleSubdomain: {
                type: 'string',
                description: 'The subdomain used for all org member handles.',
              },
              orgId: {
                type: 'string',
                description: 'The unique ID of this organization.',
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
            required: ['token', 'handle'],
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
                  'The token that was issued by an org admin to allow members to join the organization.',
              },
              password: {
                type: 'string',
                description:
                  "The password for the new member's account (required for 'password' login method).",
              },
              loginID: {
                type: 'string',
                description:
                  "Provider-specific identifier (AT Protocol handle for 'atproto', email for 'google'). Required for non-password login methods.",
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
  NetworkHabitatRelationshipCheck: {
    lexicon: 1,
    id: 'network.habitat.relationship.check',
    defs: {
      main: {
        type: 'query',
        description:
          'Check whether a subject holds a role on a space. The subject is either a user (DID) or a space-role userset (a space URI plus subjectRole), resolving through space-role usersets (groups, including org member/admin groups, are spaces, so group membership and nested groups resolve as space-role usersets) and built-in role implications (owner implies manager implies writer implies reader). Caller must have the reader role on the space.',
        parameters: {
          type: 'params',
          required: ['subject', 'relation', 'space'],
          properties: {
            subject: {
              type: 'string',
              description:
                'The subject to check: a user DID, or a space URI when checking a space-role userset. When a space URI, subjectRole is required.',
            },
            subjectRole: {
              type: 'string',
              enum: ['owner', 'manager', 'writer', 'reader'],
              description:
                'The role held on the subject space, forming a userset. Required when subject is a space URI; omit when subject is a user DID.',
            },
            relation: {
              type: 'string',
              enum: ['owner', 'manager', 'writer', 'reader'],
              description: 'The role to check for on the space.',
            },
            space: {
              type: 'string',
              format: 'uri',
              description: 'URI of the space.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['allowed'],
            properties: {
              allowed: {
                type: 'boolean',
                description: 'Whether the subject holds the role on the space.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatRelationshipDefs: {
    lexicon: 1,
    id: 'network.habitat.relationship.defs',
    defs: {
      spaceObject: {
        type: 'object',
        description: 'A space that a role is granted on.',
        required: ['space'],
        properties: {
          space: {
            type: 'string',
            format: 'uri',
            description: 'URI of the space.',
          },
        },
      },
      userSubject: {
        type: 'object',
        description: 'An individual user, identified by DID.',
        required: ['did'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
        },
      },
      spaceRoleSubject: {
        type: 'object',
        description:
          "All subjects holding a role on a space (a userset). Enables cross-space inheritance, e.g. spaceA's writers as writers of spaceB.",
        required: ['space', 'role'],
        properties: {
          space: {
            type: 'string',
            format: 'uri',
            description: 'URI of the space (or group-space).',
          },
          role: {
            type: 'string',
            enum: ['owner', 'manager', 'writer', 'reader'],
          },
        },
      },
    },
  },
  NetworkHabitatRelationshipDeleteTuple: {
    lexicon: 1,
    id: 'network.habitat.relationship.deleteTuple',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Delete a relationship tuple by its record URI. Caller must have the manager role on the tuple's governing space.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['uri'],
            properties: {
              uri: {
                type: 'string',
                description: 'URI of the tuple record to delete.',
              },
            },
          },
        },
        errors: [
          {
            name: 'TupleNotFound',
            description: 'No tuple record exists at the given URI.',
          },
        ],
      },
    },
  },
  NetworkHabitatRelationshipListObjects: {
    lexicon: 1,
    id: 'network.habitat.relationship.listObjects',
    defs: {
      main: {
        type: 'query',
        description:
          'List the spaces on which a user holds a role, expanding space-role usersets (groups, including org member/admin groups, are spaces, so group membership and nested groups resolve as space-role usersets) and built-in role implications. Returns only spaces the caller has the reader role on.',
        parameters: {
          type: 'params',
          required: ['did', 'relation'],
          properties: {
            did: {
              type: 'string',
              format: 'did',
              description: 'DID of the user.',
            },
            relation: {
              type: 'string',
              enum: ['owner', 'manager', 'writer', 'reader'],
              description: 'The role to query for.',
            },
            type: {
              type: 'string',
              format: 'nsid',
              description: 'Filter to spaces of this type.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['spaces'],
            properties: {
              spaces: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'uri',
                },
                description: 'URIs of spaces where the user holds the role.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatRelationshipListSubjects: {
    lexicon: 1,
    id: 'network.habitat.relationship.listSubjects',
    defs: {
      main: {
        type: 'query',
        description:
          'List the user DIDs that hold a role on a space, expanding space-role usersets (groups, including org member/admin groups, are spaces, so group membership and nested groups resolve as space-role usersets) and built-in role implications. Caller must have the reader role on the space.',
        parameters: {
          type: 'params',
          required: ['space', 'relation'],
          properties: {
            space: {
              type: 'string',
              format: 'uri',
              description: 'URI of the space.',
            },
            relation: {
              type: 'string',
              enum: ['owner', 'manager', 'writer', 'reader'],
              description: 'The role to expand.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['dids'],
            properties: {
              dids: {
                type: 'array',
                items: {
                  type: 'string',
                  format: 'did',
                },
                description: 'DIDs of users holding the role on the space.',
              },
            },
          },
        },
      },
    },
  },
  NetworkHabitatRelationshipListTuples: {
    lexicon: 1,
    id: 'network.habitat.relationship.listTuples',
    defs: {
      main: {
        type: 'query',
        description:
          'List relationship tuples governing a space, optionally filtered by object, subject, subject type, or relation. Caller must have the reader role on the space. This is the interoperable read surface other apps use to understand the permission structure.',
        parameters: {
          type: 'params',
          required: ['space'],
          properties: {
            space: {
              type: 'string',
              format: 'uri',
              description: 'URI of the governing space whose tuples to list.',
            },
            object: {
              type: 'string',
              format: 'uri',
              description:
                'Optional. Restrict to tuples whose object is this space or group URI.',
            },
            subjectDid: {
              type: 'string',
              format: 'did',
              description:
                'Optional. Restrict to tuples whose subject is this user DID.',
            },
            subjectType: {
              type: 'string',
              enum: ['user', 'space'],
              description:
                'Optional. Restrict to tuples whose subject is a user (userSubject) or a space userset (spaceRoleSubject).',
            },
            relation: {
              type: 'string',
              description: 'Optional. Restrict to tuples with this relation.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['tuples'],
            properties: {
              tuples: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.relationship.listTuples#tupleView',
                },
              },
            },
          },
        },
      },
      tupleView: {
        type: 'object',
        required: ['uri', 'subject', 'relation', 'object'],
        properties: {
          uri: {
            type: 'string',
            description: 'URI of the tuple record.',
          },
          subject: {
            type: 'union',
            refs: [
              'lex:network.habitat.relationship.defs#userSubject',
              'lex:network.habitat.relationship.defs#spaceRoleSubject',
            ],
          },
          relation: {
            type: 'string',
          },
          object: {
            type: 'ref',
            ref: 'lex:network.habitat.relationship.defs#spaceObject',
          },
        },
      },
    },
  },
  NetworkHabitatRelationshipTuple: {
    lexicon: 1,
    id: 'network.habitat.relationship.tuple',
    defs: {
      main: {
        type: 'record',
        description:
          'A relationship tuple (subject, relation, object) defining one access-control relationship. The object is always a space; groups are spaces too, so granting a role on a group-space is just an ordinary tuple. Owned by the org repo within the space it governs so authorized app users can manage it and other apps can read the permission structure.',
        key: 'tid',
        record: {
          type: 'object',
          required: ['subject', 'relation', 'object'],
          properties: {
            subject: {
              type: 'union',
              refs: [
                'lex:network.habitat.relationship.defs#userSubject',
                'lex:network.habitat.relationship.defs#spaceRoleSubject',
              ],
            },
            relation: {
              type: 'string',
              knownValues: ['owner', 'manager', 'writer', 'reader'],
              description:
                'Role granted on the object space (owner|manager|writer|reader).',
            },
            object: {
              type: 'ref',
              ref: 'lex:network.habitat.relationship.defs#spaceObject',
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
  NetworkHabitatRelationshipWriteTuple: {
    lexicon: 1,
    id: 'network.habitat.relationship.writeTuple',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Write a relationship tuple, creating it if it does not already exist. The tuple record is owned by the org repo within its governing space. Caller must have the manager role on the object space.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['subject', 'relation', 'object'],
            properties: {
              subject: {
                type: 'union',
                refs: [
                  'lex:network.habitat.relationship.defs#userSubject',
                  'lex:network.habitat.relationship.defs#spaceRoleSubject',
                ],
              },
              relation: {
                type: 'string',
                knownValues: ['owner', 'manager', 'writer', 'reader'],
                description:
                  'Role granted on the object space (owner|manager|writer|reader).',
              },
              object: {
                type: 'ref',
                ref: 'lex:network.habitat.relationship.defs#spaceObject',
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
                description: 'URI of the written tuple record.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
            description: 'The object space does not exist.',
          },
          {
            name: 'InvalidTuple',
            description:
              'The subject, relation, and object combination is not valid.',
          },
        ],
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
  NetworkHabitatSearchQuery: {
    lexicon: 1,
    id: 'network.habitat.search.query',
    defs: {
      main: {
        type: 'query',
        description:
          "Full-text search over records the caller's org has indexed.",
        parameters: {
          type: 'params',
          properties: {
            q: {
              type: 'string',
              description: 'The search query text.',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 100,
              default: 25,
            },
            cursor: {
              type: 'string',
            },
          },
          required: ['q'],
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['results'],
            properties: {
              results: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.search.query#resultView',
                },
              },
              cursor: {
                type: 'string',
              },
            },
          },
        },
      },
      resultView: {
        type: 'object',
        required: ['uri', 'spaceUri', 'recordType'],
        properties: {
          uri: {
            type: 'string',
            description: 'URI of the matched record.',
          },
          spaceUri: {
            type: 'string',
            description: 'URI of the space the record belongs to.',
          },
          recordType: {
            type: 'string',
            format: 'nsid',
            description: 'The NSID of the record type.',
          },
          snippet: {
            type: 'string',
            description: 'A highlighted excerpt of the matching content.',
          },
          rank: {
            type: 'integer',
            description:
              'Relevance score scaled by 1,000,000, higher is more relevant.',
          },
        },
      },
    },
  },
  NetworkHabitatSpaceAddMember: {
    lexicon: 1,
    id: 'network.habitat.space.addMember',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Add a member to a space. Caller must have can_manage_members.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['space', 'did'],
            properties: {
              space: {
                type: 'string',
                format: 'uri',
                description: 'Reference to the space.',
              },
              did: {
                type: 'string',
                format: 'did',
                description: 'The DID of the user to add.',
              },
              access: {
                type: 'string',
                enum: ['read', 'write'],
                default: 'read',
                description:
                  'The access level to give the user. Defaults to read.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
            description: 'The specified space does not exist.',
          },
          {
            name: 'UserAlreadyMember',
            description: 'The user is already a member of the space.',
          },
        ],
      },
    },
  },
  NetworkHabitatSpaceCreateSpace: {
    lexicon: 1,
    id: 'network.habitat.space.createSpace',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Create a new space. The authenticated user becomes the space owner. Requires auth, implemented by PDS.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['type'],
            properties: {
              type: {
                type: 'string',
                format: 'nsid',
                description:
                  'The NSID of the space type, describing the modality of the space.',
              },
              skey: {
                type: 'string',
                maxLength: 512,
                description:
                  'The space key. Used to differentiate multiple spaces of the same type under the same owner. If not provided, one will be auto-generated.',
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
                description: 'URI of the created space.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceAlreadyExists',
            description:
              'A space with this owner, type, and skey already exists.',
          },
          {
            name: 'InvalidType',
            description:
              'The provided space type NSID is not a recognized or valid space type.',
          },
        ],
      },
    },
  },
  NetworkHabitatSpaceDeleteRecord: {
    lexicon: 1,
    id: 'network.habitat.space.deleteRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          "Delete a record in a space, or ensure it doesn't exist. Caller must have can_delete on the space.",
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['space', 'collection', 'rkey'],
            nullable: ['swapRecord', 'swapCommit'],
            properties: {
              space: {
                type: 'string',
                format: 'uri',
                description: 'Reference to the space.',
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
            properties: {},
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
  NetworkHabitatSpaceDeleteSpace: {
    lexicon: 1,
    id: 'network.habitat.space.deleteSpace',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Delete an entire space. Only the space owner can delete. All records in the space and all member relationships are removed.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['space'],
            properties: {
              space: {
                type: 'string',
                format: 'uri',
                description: 'URI of the space to delete.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
            description: 'The specified space does not exist.',
          },
        ],
      },
    },
  },
  NetworkHabitatSpaceGetRecord: {
    lexicon: 1,
    id: 'network.habitat.space.getRecord',
    defs: {
      main: {
        type: 'query',
        description:
          "Get a single record from a permissioned space. Callable with either OAuth (for the authenticated user's own data) or a space credential (for syncing services).",
        parameters: {
          type: 'params',
          required: ['space', 'repo', 'collection', 'rkey'],
          properties: {
            space: {
              type: 'string',
              format: 'at-uri',
              description: 'Reference to the space.',
            },
            repo: {
              type: 'string',
              format: 'did',
              description: 'The DID of the account whose repo to read from.',
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
        output: {
          encoding: 'application/json',
          schema: {
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
        errors: [
          {
            name: 'RecordNotFound',
          },
          {
            name: 'SpaceNotFound',
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
  NetworkHabitatSpaceGetRepoOplog: {
    lexicon: 1,
    id: 'network.habitat.space.getRepoOplog',
    defs: {
      main: {
        type: 'query',
        description:
          'Get records modified since a given revision for a member in a space. Used for incremental sync. Callable by any member of the space.',
        parameters: {
          type: 'params',
          required: ['space', 'repo'],
          properties: {
            space: {
              type: 'string',
              format: 'uri',
              description: 'Reference to the space.',
            },
            repo: {
              type: 'string',
              format: 'did',
              description: 'The DID of the member whose records to track.',
            },
            since: {
              type: 'string',
              description:
                'Return records with revisions after this value (exclusive).',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 1000,
              default: 100,
              description: 'Maximum number of records to return.',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['records'],
            properties: {
              records: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.space.getRepoOplog#record',
                },
              },
              cursor: {
                type: 'string',
                description:
                  'The revision of the last returned record. Use as `since` in the next poll.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
          },
        ],
      },
      record: {
        type: 'object',
        required: ['rev', 'collection', 'rkey', 'value'],
        properties: {
          rev: {
            type: 'string',
            description: 'Revision (TID) of this record.',
          },
          collection: {
            type: 'string',
            format: 'nsid',
          },
          rkey: {
            type: 'string',
            format: 'record-key',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
          value: {
            type: 'unknown',
            description: 'The record value.',
          },
        },
      },
    },
  },
  NetworkHabitatSpaceListRecords: {
    lexicon: 1,
    id: 'network.habitat.space.listRecords',
    defs: {
      main: {
        type: 'query',
        description:
          "List the records in an account's repo within a permissioned space, optionally filtered by collection. By default each record's value is inlined; set excludeValues for a metadata-only listing (collection, rkey, cid). Used for full-state recovery. Callable with either OAuth (for the authenticated user's own data) or a space credential (for syncing services).",
        parameters: {
          type: 'params',
          required: ['space', 'repo'],
          properties: {
            space: {
              type: 'string',
              format: 'at-uri',
              description: 'Reference to the space.',
            },
            repo: {
              type: 'string',
              format: 'did',
              description: 'The DID of the account whose repo to list.',
            },
            collection: {
              type: 'string',
              format: 'nsid',
              description:
                'The NSID of the record collection. If omitted, lists records across all collections.',
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
            excludeValues: {
              type: 'boolean',
              default: false,
              description:
                'If true, omit inlined record values and return only metadata (collection, rkey, cid).',
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
                  ref: 'lex:network.habitat.space.listRecords#record',
                },
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
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
      record: {
        type: 'object',
        required: ['collection', 'rkey', 'cid'],
        properties: {
          collection: {
            type: 'string',
            format: 'nsid',
          },
          rkey: {
            type: 'string',
            format: 'record-key',
          },
          cid: {
            type: 'string',
            format: 'cid',
          },
          value: {
            type: 'unknown',
            description:
              "The record's value. Inlined by default; omitted when excludeValues is set.",
          },
        },
      },
    },
  },
  NetworkHabitatSpaceListRepos: {
    lexicon: 1,
    id: 'network.habitat.space.listRepos',
    defs: {
      main: {
        type: 'query',
        description:
          "List the known repos that hold data in a space (the writer set), with each repo's current rev and commit hash. Served by the space host. This is the sync boundary, not an access-control list: it enumerates only writers, never readers. The set is what the authority claims from write notifications and is not itself authoritative; a repo's host is the source of truth.",
        parameters: {
          type: 'params',
          required: ['space'],
          properties: {
            space: {
              type: 'string',
              format: 'at-uri',
              description: 'Reference to the space.',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 1000,
              default: 100,
              description: 'Maximum number of repos to return.',
            },
            cursor: {
              type: 'string',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['repos'],
            properties: {
              cursor: {
                type: 'string',
              },
              repos: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.space.listRepos#repo',
                },
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
          },
        ],
      },
      repo: {
        type: 'object',
        required: ['did'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
            description: 'The DID of a repo that holds data in the space.',
          },
          rev: {
            type: 'string',
            description:
              "The repo's current revision (TID), as last reported to the authority. May lag the repo host, which is the source of truth.",
          },
          hash: {
            type: 'bytes',
            description:
              "The repo's current commit hash (sha256 of the LtHash state), as last reported to the authority.",
          },
        },
      },
    },
  },
  NetworkHabitatSpaceListSpaces: {
    lexicon: 1,
    id: 'network.habitat.space.listSpaces',
    defs: {
      main: {
        type: 'query',
        description:
          'List the spaces that the authenticated user participates in, optionally filtered by type and/or owner DID. Requires auth.',
        parameters: {
          type: 'params',
          properties: {
            type: {
              type: 'string',
              format: 'nsid',
              description: 'Filter to spaces of this type.',
            },
            did: {
              type: 'string',
              format: 'did',
              description: 'Filter to spaces owned by this DID.',
            },
            limit: {
              type: 'integer',
              minimum: 1,
              maximum: 100,
              default: 50,
            },
            cursor: {
              type: 'string',
            },
          },
        },
        output: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['spaces'],
            properties: {
              cursor: {
                type: 'string',
              },
              spaces: {
                type: 'array',
                items: {
                  type: 'ref',
                  ref: 'lex:network.habitat.space.listSpaces#spaceView',
                },
              },
            },
          },
        },
      },
      spaceView: {
        type: 'object',
        required: ['uri', 'type'],
        properties: {
          uri: {
            type: 'string',
            description: 'URI of the space.',
          },
          type: {
            type: 'string',
            format: 'nsid',
            description: 'The NSID of the space type.',
          },
          skey: {
            type: 'string',
            description: 'The space key.',
          },
          memberCount: {
            type: 'integer',
            description: 'Number of members in the space.',
          },
        },
      },
    },
  },
  NetworkHabitatSpacePutRecord: {
    lexicon: 1,
    id: 'network.habitat.space.putRecord',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Write a record in a permissioned space, creating or updating it as needed. Requires auth, implemented by PDS.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['space', 'repo', 'collection', 'record'],
            properties: {
              space: {
                type: 'string',
                format: 'at-uri',
                description: 'Reference to the space.',
              },
              repo: {
                type: 'string',
                format: 'did',
                description:
                  'The DID of the repo to write to (the authenticated member).',
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
            required: ['uri', 'cid'],
            properties: {
              uri: {
                type: 'string',
                format: 'at-uri',
                description: 'URI of the written record.',
              },
              cid: {
                type: 'string',
                format: 'cid',
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
            name: 'SpaceNotFound',
          },
        ],
      },
    },
  },
  NetworkHabitatSpaceRemoveMember: {
    lexicon: 1,
    id: 'network.habitat.space.removeMember',
    defs: {
      main: {
        type: 'procedure',
        description:
          'Remove a member from a space. Caller must have can_manage_members.',
        input: {
          encoding: 'application/json',
          schema: {
            type: 'object',
            required: ['space', 'did'],
            properties: {
              space: {
                type: 'string',
                format: 'uri',
                description: 'Reference to the space.',
              },
              did: {
                type: 'string',
                format: 'did',
                description: 'The DID of the user to remove.',
              },
            },
          },
        },
        errors: [
          {
            name: 'SpaceNotFound',
            description: 'The specified space does not exist.',
          },
          {
            name: 'NotAMember',
            description: 'The specified user is not a member of the space.',
          },
        ],
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
  ComAtprotoRepoDescribeRepo: 'com.atproto.repo.describeRepo',
  ComAtprotoRepoGetRecord: 'com.atproto.repo.getRecord',
  ComAtprotoRepoListRecords: 'com.atproto.repo.listRecords',
  ComAtprotoRepoPutRecord: 'com.atproto.repo.putRecord',
  ComAtprotoRepoStrongRef: 'com.atproto.repo.strongRef',
  ComAtprotoServerGetServiceAuth: 'com.atproto.server.getServiceAuth',
  CommunityLexiconCalendarEvent: 'community.lexicon.calendar.event',
  CommunityLexiconCalendarInvite: 'community.lexicon.calendar.invite',
  CommunityLexiconCalendarRsvp: 'community.lexicon.calendar.rsvp',
  CommunityLexiconLocationAddress: 'community.lexicon.location.address',
  CommunityLexiconLocationFsq: 'community.lexicon.location.fsq',
  CommunityLexiconLocationGeo: 'community.lexicon.location.geo',
  CommunityLexiconLocationHthree: 'community.lexicon.location.hthree',
  NetworkHabitatAdminGetSettings: 'network.habitat.admin.getSettings',
  NetworkHabitatAdminIssueInvite: 'network.habitat.admin.issueInvite',
  NetworkHabitatAdminUpdateSettings: 'network.habitat.admin.updateSettings',
  NetworkHabitatClique: 'network.habitat.clique',
  NetworkHabitatCliqueAddMembers: 'network.habitat.clique.addMembers',
  NetworkHabitatCliqueCreateClique: 'network.habitat.clique.createClique',
  NetworkHabitatCliqueGetMembers: 'network.habitat.clique.getMembers',
  NetworkHabitatCliqueIsMember: 'network.habitat.clique.isMember',
  NetworkHabitatCliqueRemoveMembers: 'network.habitat.clique.removeMembers',
  NetworkHabitatCollectionsDefs: 'network.habitat.collections.defs',
  NetworkHabitatCollectionsListCollections:
    'network.habitat.collections.listCollections',
  NetworkHabitatCollectionsListRecords:
    'network.habitat.collections.listRecords',
  NetworkHabitatDocsCrdt: 'network.habitat.docs.crdt',
  NetworkHabitatDocsCreateDoc: 'network.habitat.docs.createDoc',
  NetworkHabitatDocsListDocs: 'network.habitat.docs.listDocs',
  NetworkHabitatDocsMarkdown: 'network.habitat.docs.markdown',
  NetworkHabitatDocsUpdateDoc: 'network.habitat.docs.updateDoc',
  NetworkHabitatGrantee: 'network.habitat.grantee',
  NetworkHabitatGroupProfile: 'network.habitat.group.profile',
  NetworkHabitatGroupsAddMember: 'network.habitat.groups.addMember',
  NetworkHabitatGroupsCreateGroup: 'network.habitat.groups.createGroup',
  NetworkHabitatGroupsDefs: 'network.habitat.groups.defs',
  NetworkHabitatGroupsDeleteMember: 'network.habitat.groups.deleteMember',
  NetworkHabitatGroupsGetGroup: 'network.habitat.groups.getGroup',
  NetworkHabitatGroupsListGroups: 'network.habitat.groups.listGroups',
  NetworkHabitatGroupsUpdateGroup: 'network.habitat.groups.updateGroup',
  NetworkHabitatInstanceDescribeInstance:
    'network.habitat.instance.describeInstance',
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
  NetworkHabitatRelationshipCheck: 'network.habitat.relationship.check',
  NetworkHabitatRelationshipDefs: 'network.habitat.relationship.defs',
  NetworkHabitatRelationshipDeleteTuple:
    'network.habitat.relationship.deleteTuple',
  NetworkHabitatRelationshipListObjects:
    'network.habitat.relationship.listObjects',
  NetworkHabitatRelationshipListSubjects:
    'network.habitat.relationship.listSubjects',
  NetworkHabitatRelationshipListTuples:
    'network.habitat.relationship.listTuples',
  NetworkHabitatRelationshipTuple: 'network.habitat.relationship.tuple',
  NetworkHabitatRelationshipWriteTuple:
    'network.habitat.relationship.writeTuple',
  NetworkHabitatRenderSchema: 'network.habitat.render.schema',
  NetworkHabitatRepoCreateRecord: 'network.habitat.repo.createRecord',
  NetworkHabitatRepoDeleteRecord: 'network.habitat.repo.deleteRecord',
  NetworkHabitatRepoDescribeRepo: 'network.habitat.repo.describeRepo',
  NetworkHabitatRepoGetBlob: 'network.habitat.repo.getBlob',
  NetworkHabitatRepoGetRecord: 'network.habitat.repo.getRecord',
  NetworkHabitatRepoListRecords: 'network.habitat.repo.listRecords',
  NetworkHabitatRepoPutRecord: 'network.habitat.repo.putRecord',
  NetworkHabitatRepoUploadBlob: 'network.habitat.repo.uploadBlob',
  NetworkHabitatSearchQuery: 'network.habitat.search.query',
  NetworkHabitatSpaceAddMember: 'network.habitat.space.addMember',
  NetworkHabitatSpaceCreateSpace: 'network.habitat.space.createSpace',
  NetworkHabitatSpaceDeleteRecord: 'network.habitat.space.deleteRecord',
  NetworkHabitatSpaceDeleteSpace: 'network.habitat.space.deleteSpace',
  NetworkHabitatSpaceGetRecord: 'network.habitat.space.getRecord',
  NetworkHabitatSpaceGetRepoOplog: 'network.habitat.space.getRepoOplog',
  NetworkHabitatSpaceListRecords: 'network.habitat.space.listRecords',
  NetworkHabitatSpaceListRepos: 'network.habitat.space.listRepos',
  NetworkHabitatSpaceListSpaces: 'network.habitat.space.listSpaces',
  NetworkHabitatSpacePutRecord: 'network.habitat.space.putRecord',
  NetworkHabitatSpaceRemoveMember: 'network.habitat.space.removeMember',
} as const
