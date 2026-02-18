/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../lexicons'
import { type $Typed, is$typed as _is$typed, type OmitKey } from '../../../util'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.grantee'

/** A DID grantee */
export type DidGrantee = string
/** A clique ref grantee in the form habitat://did:plc:web:arushi/habitat.network.clique/clique-record-key */
export type CliqueRef = string
