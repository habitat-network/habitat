/**
 * A Habitat app's definition: the slice of an org's space data it syncs, and
 * the fields and indexes it queries that data by.
 *
 * The slice is stored as one generic table of JSON records (see schema.ts),
 * so syncing needs no knowledge of this definition. The definition only
 * decides which collections are kept, and which JSON paths get an index.
 */

/** A declared field: a JSON path into the record, e.g. `$.createdAt`. */
export type FieldPath = `$.${string}`;

export interface CollectionDefinition {
  /**
   * Named fields queries may filter and order by, each a JSON path into the
   * record value. Paths are restricted to dotted identifiers (no array
   * steps): they are inlined into index expressions.
   */
  readonly fields?: Readonly<Record<string, FieldPath>>;
  /**
   * Indexes over this collection's records. Each is an ordered list of
   * declared field names or the built-in columns `space` and `repo`. Each
   * becomes a partial expression index, so adding one later is an index
   * build over the records already synced, not a re-sync.
   */
  readonly indexes?: ReadonlyArray<ReadonlyArray<string>>;
}

export interface AppDefinition {
  /** A short identifier for the app, for logs and index names. */
  readonly name: string;
  /** The space types (NSIDs) whose spaces this app syncs. */
  readonly spaceTypes: ReadonlyArray<string>;
  /** The record collections (NSIDs) kept in the slice, with their fields. */
  readonly collections: Readonly<Record<string, CollectionDefinition>>;
  /**
   * Opt in to `ctx.unsafeDb`, which reads the slice without filtering to the
   * viewer's readable spaces (for org-wide aggregates). Off by default, and
   * visible here so whoever reviews the app sees that it asks for it.
   */
  readonly unsafeCrossSpaceReads?: boolean;
}

export interface HabitatApp<D extends AppDefinition = AppDefinition> {
  readonly definition: D;
}

/** Columns every record has, usable in `where`, `orderBy` and indexes. */
export const BUILTIN_COLUMNS = ["space", "repo", "rkey", "uri"] as const;
export type BuiltinColumn = (typeof BUILTIN_COLUMNS)[number];

const NSID =
  /^[a-zA-Z]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z]([a-zA-Z0-9-]*[a-zA-Z0-9])?){2,}$/;
const FIELD_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
// Dotted identifiers only: the path is inlined into SQL (an index expression
// can't take a bound parameter), so nothing that needs quoting is allowed.
const FIELD_PATH = /^\$(\.[A-Za-z_][A-Za-z0-9_]*)+$/;

export class AppDefinitionError extends Error {}

/**
 * defineApp validates a definition and returns the app. It throws
 * AppDefinitionError for anything that couldn't be turned into safe SQL.
 */
export function defineApp<const D extends AppDefinition>(
  definition: D,
): HabitatApp<D> {
  if (!FIELD_NAME.test(definition.name)) {
    throw new AppDefinitionError(`invalid app name: ${definition.name}`);
  }
  for (const type of definition.spaceTypes) {
    if (!NSID.test(type)) {
      throw new AppDefinitionError(`invalid space type: ${type}`);
    }
  }
  for (const [nsid, collection] of Object.entries(definition.collections)) {
    if (!NSID.test(nsid)) {
      throw new AppDefinitionError(`invalid collection: ${nsid}`);
    }
    const fields = collection.fields ?? {};
    for (const [name, path] of Object.entries(fields)) {
      if (!FIELD_NAME.test(name)) {
        throw new AppDefinitionError(`${nsid}: invalid field name: ${name}`);
      }
      if ((BUILTIN_COLUMNS as readonly string[]).includes(name)) {
        throw new AppDefinitionError(
          `${nsid}: field ${name} shadows a built-in column`,
        );
      }
      if (!FIELD_PATH.test(path)) {
        throw new AppDefinitionError(
          `${nsid}: field ${name} has an unsupported path: ${path}`,
        );
      }
    }
    for (const index of collection.indexes ?? []) {
      if (index.length === 0) {
        throw new AppDefinitionError(`${nsid}: empty index`);
      }
      for (const column of index) {
        if (
          !(column in fields) &&
          !(BUILTIN_COLUMNS as readonly string[]).includes(column)
        ) {
          throw new AppDefinitionError(
            `${nsid}: index names unknown field: ${column}`,
          );
        }
      }
    }
  }
  return { definition };
}

/**
 * columnExpression is the SQL for one column of a collection: a built-in
 * column name, or the json_extract of a declared field. Queries and indexes
 * both go through this, so the planner sees the identical expression.
 */
export function columnExpression(
  app: HabitatApp,
  collection: string,
  column: string,
): string {
  if ((BUILTIN_COLUMNS as readonly string[]).includes(column)) return column;
  const path = app.definition.collections[collection]?.fields?.[column];
  if (path === undefined) {
    throw new AppDefinitionError(
      `${collection} has no declared field named ${column}`,
    );
  }
  return `json_extract(value, '${path}')`;
}
