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
const id = 'network.habitat.collections.defs'

/** A record collection (lexicon type) present in the org's synced data, with a count of the records in it the calling user can see. A record scoped to more than one readable space is counted once per space, since each space holds its own version. */
export interface CollectionView {
  $type?: 'network.habitat.collections.defs#collectionView'
  /** The NSID of the record collection. */
  collection: string
  /** Number of records in this collection the calling user can see, counted across all spaces they can read (once per space a record belongs to). */
  recordCount: number
}

const hashCollectionView = 'collectionView'

export function isCollectionView<V>(v: V) {
  return is$typed(v, id, hashCollectionView)
}

export function validateCollectionView<V>(v: V) {
  return validate<CollectionView & V>(v, id, hashCollectionView)
}

/** A single record scoped to one space. The same repo/collection/rkey in a different space is a distinct record with its own version, so it appears as its own recordView. The record body is not included; fetch it on demand from pear using the space, repo, collection and rkey. */
export interface RecordView {
  $type?: 'network.habitat.collections.defs#recordView'
  /** The space-record URI (spaceUri/repo/collection/rkey), unique to this record in this space. */
  uri: string
  /** URI of the space this record belongs to. */
  space: string
  /** DID of the repo the record lives in. */
  repo: string
  /** The NSID of the record collection. */
  collection: string
  /** The record key. */
  rkey: string
}

const hashRecordView = 'recordView'

export function isRecordView<V>(v: V) {
  return is$typed(v, id, hashRecordView)
}

export function validateRecordView<V>(v: V) {
  return validate<RecordView & V>(v, id, hashRecordView)
}
