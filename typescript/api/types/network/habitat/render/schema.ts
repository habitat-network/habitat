/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.render.schema'

export interface Main {
  $type: 'network.habitat.render.schema'
  /** The NSID of the lexicon this render schema applies to. */
  targetLexicon: string
  /** Human-readable name for this record type. */
  title: string
  /** A brief description of what this record type represents. */
  description?: string
  /** Ordered list of field display descriptors. */
  fields: FieldSchema[]
  [k: string]: unknown
}

const hashMain = 'main'

export function isMain<V>(v: V) {
  return is$typed(v, id, hashMain)
}

export function validateMain<V>(v: V) {
  return validate<Main & V>(v, id, hashMain, true)
}

export {
  type Main as Record,
  isMain as isRecord,
  validateMain as validateRecord,
}

/** Describes how to display a single field of a record. */
export interface FieldSchema {
  $type?: 'network.habitat.render.schema#fieldSchema'
  /** Dot-notation path into the record value (e.g. 'name', 'startsAt'). */
  path: string
  /** Human-readable label for this field. */
  label: string
  /** How to render the value. */
  displayType:
    | 'network.habitat.render.schema#text'
    | 'network.habitat.render.schema#datetime'
    | 'network.habitat.render.schema#url'
    | 'network.habitat.render.schema#badge'
    | 'network.habitat.render.schema#list'
    | (string & {})
  /** Layout prominence of this field. */
  priority:
    | 'network.habitat.render.schema#primary'
    | 'network.habitat.render.schema#secondary'
    | 'network.habitat.render.schema#metadata'
    | (string & {})
  /** If true, omit this field from display when its value is missing or empty. */
  optional: boolean
}

const hashFieldSchema = 'fieldSchema'

export function isFieldSchema<V>(v: V) {
  return is$typed(v, id, hashFieldSchema)
}

export function validateFieldSchema<V>(v: V) {
  return validate<FieldSchema & V>(v, id, hashFieldSchema)
}

/** Render as plain text. */
export const TEXT = `${id}#text`
/** Render as a formatted date/time string. */
export const DATETIME = `${id}#datetime`
/** Render as a hyperlink. */
export const URL = `${id}#url`
/** Render as a pill badge, extracting the token name from an NSID#token value. */
export const BADGE = `${id}#badge`
/** Render as a list of items. */
export const LIST = `${id}#list`
/** Most prominent display — used for the record's main identifier (e.g. title). */
export const PRIMARY = `${id}#primary`
/** Standard field-value display. */
export const SECONDARY = `${id}#secondary`
/** De-emphasized display, shown at the bottom or collapsed. */
export const METADATA = `${id}#metadata`
