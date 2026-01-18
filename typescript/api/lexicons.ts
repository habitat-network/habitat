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
                ref: 'lex:network.habitat.notification.createNotification#notification',
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
            ref: 'lex:network.habitat.notification.listNotifications#notification',
          },
        },
      },
      notification: {
        type: 'object',
        required: ['did', 'originDid', 'collection', 'rkey'],
        properties: {
          did: {
            type: 'string',
            format: 'did',
          },
          originDid: {
            type: 'string',
            format: 'did',
          },
          collection: {
            type: 'string',
            format: 'nsid',
          },
          rkey: {
            type: 'string',
            format: 'record-key',
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
              grantees: {
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
  NetworkHabitatNotificationCreateNotification:
    'network.habitat.notification.createNotification',
  NetworkHabitatNotificationListNotifications:
    'network.habitat.notification.listNotifications',
  NetworkHabitatPhoto: 'network.habitat.photo',
  NetworkHabitatRepoGetBlob: 'network.habitat.repo.getBlob',
  NetworkHabitatRepoGetRecord: 'network.habitat.repo.getRecord',
  NetworkHabitatRepoListRecords: 'network.habitat.repo.listRecords',
  NetworkHabitatRepoPutRecord: 'network.habitat.repo.putRecord',
  NetworkHabitatRepoUploadBlob: 'network.habitat.repo.uploadBlob',
} as const
