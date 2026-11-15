import { columnExpression, type HabitatApp } from "./definition.js";
import { parseSpace } from "./sink.js";
import type { SqlDatabase } from "./sql.js";

/** A record as the slice holds it. */
export interface SliceRecord<T = unknown> {
  readonly uri: string;
  readonly cid: string;
  readonly space: string;
  /** The DID of the member whose repo holds the record: its author. */
  readonly repo: string;
  readonly collection: string;
  readonly rkey: string;
  readonly value: T;
}

export type Comparison = "=" | "!=" | "<" | "<=" | ">" | ">=";
const COMPARISONS: readonly string[] = ["=", "!=", "<", "<=", ">", ">="];

// The spaces a member can read, mirroring pear's own check
// (internal/opensocial's CheckPermission): the space's access record names
// a role the member's membership record holds. A space with no access
// record, or one this slice hasn't synced the membership for, is readable
// by nobody.
const READABLE_SPACES = `SELECT sr.space FROM hab_space_roles sr
  JOIN hab_member_roles mr ON mr.org = sr.org AND mr.role = sr.role
  WHERE mr.did = ?`;

interface RawRow {
  uri: string;
  cid: string;
  space: string;
  repo: string;
  collection: string;
  rkey: string;
  value: string;
}

function toRecord<T>(row: RawRow): SliceRecord<T> {
  return { ...row, value: JSON.parse(row.value) as T };
}

/** Marks a SliceDb that deliberately skips the viewer filter. */
export const UNSAFE_ALL_SPACES = Symbol("habitat.unsafeAllSpaces");
export type Viewer = string | typeof UNSAFE_ALL_SPACES;

/**
 * SliceDb reads the slice as one viewer. Every read it offers is restricted
 * to the spaces that viewer can read; there is no way to construct one
 * without naming a viewer, and the only unfiltered form is the explicit
 * UNSAFE_ALL_SPACES (ctx.unsafeDb), which an app must opt in to.
 */
export class SliceDb {
  constructor(
    private readonly db: SqlDatabase,
    private readonly app: HabitatApp,
    private readonly viewer: Viewer,
  ) {}

  /** Starts a query over one collection's records. */
  query<T = unknown>(collection: string): SliceQuery<T> {
    if (!(collection in this.app.definition.collections)) {
      throw new Error(`${collection} is not a collection of this app`);
    }
    return new SliceQuery<T>(this.db, this.app, this.viewer, collection);
  }

  /** Reads one record by URI, or null when it's absent or not readable. */
  async get<T = unknown>(uri: string): Promise<SliceRecord<T> | null> {
    const scope = this.scope();
    const { results } = await this.db
      .prepare(`SELECT * FROM hab_records WHERE uri = ?${scope.sql} LIMIT 1`)
      .bind(uri, ...scope.params)
      .all<RawRow>();
    return results[0] ? toRecord<T>(results[0]) : null;
  }

  /** Whether the viewer can read a space. */
  async canRead(space: string): Promise<boolean> {
    if (this.viewer === UNSAFE_ALL_SPACES) return true;
    const { results } = await this.db
      .prepare(
        `SELECT 1 AS ok FROM (${READABLE_SPACES}) WHERE space = ? LIMIT 1`,
      )
      .bind(this.viewer, space)
      .all();
    return results.length > 0;
  }

  /**
   * Lists the spaces the viewer can read, optionally only those of one
   * space type or under one authority (an org's DID).
   */
  async spaces(
    filter: { type?: string; authority?: string } = {},
  ): Promise<string[]> {
    const sql =
      this.viewer === UNSAFE_ALL_SPACES
        ? `SELECT DISTINCT space FROM hab_space_roles ORDER BY space`
        : `SELECT DISTINCT space FROM (${READABLE_SPACES}) ORDER BY space`;
    const params = this.viewer === UNSAFE_ALL_SPACES ? [] : [this.viewer];
    const { results } = await this.db
      .prepare(sql)
      .bind(...params)
      .all<{ space: string }>();
    return results
      .map((r) => r.space)
      .filter((space) => {
        const parsed = parseSpace(space);
        if (!parsed) return false;
        if (filter.type !== undefined && parsed.type !== filter.type)
          return false;
        if (
          filter.authority !== undefined &&
          parsed.authority !== filter.authority
        )
          return false;
        return true;
      });
  }

  private scope(): { sql: string; params: unknown[] } {
    return this.viewer === UNSAFE_ALL_SPACES
      ? { sql: "", params: [] }
      : { sql: ` AND space IN (${READABLE_SPACES})`, params: [this.viewer] };
  }
}

/**
 * SliceQuery builds one read over a collection. Conditions and ordering name
 * declared fields (or built-in columns); each is emitted as the same
 * expression its index was built on.
 */
export class SliceQuery<T> {
  private readonly conditions: string[] = [];
  private readonly params: unknown[] = [];
  private order: string | undefined;
  private max: number | undefined;

  constructor(
    private readonly db: SqlDatabase,
    private readonly app: HabitatApp,
    private readonly viewer: Viewer,
    private readonly collection: string,
  ) {}

  /** Narrows to one space. The viewer filter still applies. */
  in(space: string): this {
    return this.where("space", "=", space);
  }

  where(
    column: string,
    comparison: Comparison,
    value: string | number | boolean | null,
  ): this {
    if (!COMPARISONS.includes(comparison)) {
      throw new Error(`unsupported comparison: ${String(comparison)}`);
    }
    const expression = columnExpression(this.app, this.collection, column);
    this.conditions.push(`${expression} ${comparison} ?`);
    this.params.push(typeof value === "boolean" ? Number(value) : value);
    return this;
  }

  orderBy(column: string, direction: "asc" | "desc" = "asc"): this {
    const expression = columnExpression(this.app, this.collection, column);
    const dir = direction === "desc" ? "DESC" : "ASC";
    // uri breaks ties so the order is total.
    this.order = `${expression} ${dir}, uri ${dir}`;
    return this;
  }

  limit(count: number): this {
    if (!Number.isInteger(count) || count < 0) {
      throw new Error(`invalid limit: ${count}`);
    }
    this.max = count;
    return this;
  }

  async collect(): Promise<SliceRecord<T>[]> {
    const { sql, params } = this.build("*");
    const { results } = await this.db
      .prepare(sql)
      .bind(...params)
      .all<RawRow>();
    return results.map((row) => toRecord<T>(row));
  }

  async first(): Promise<SliceRecord<T> | null> {
    this.max = 1;
    return (await this.collect())[0] ?? null;
  }

  async count(): Promise<number> {
    const { sql, params } = this.build("COUNT(*) AS n", false);
    const { results } = await this.db
      .prepare(sql)
      .bind(...params)
      .all<{ n: number }>();
    return results[0]?.n ?? 0;
  }

  /** The SQL this query runs, for tests and EXPLAIN. */
  toSQL(): { sql: string; params: unknown[] } {
    return this.build("*");
  }

  private build(
    select: string,
    ordered = true,
  ): { sql: string; params: unknown[] } {
    const where = [`collection = ?`];
    const params: unknown[] = [this.collection];
    if (this.viewer !== UNSAFE_ALL_SPACES) {
      where.push(`space IN (${READABLE_SPACES})`);
      params.push(this.viewer);
    }
    where.push(...this.conditions);
    params.push(...this.params);
    let sql = `SELECT ${select} FROM hab_records WHERE ${where.join(" AND ")}`;
    if (ordered) {
      if (this.order) sql += ` ORDER BY ${this.order}`;
      if (this.max !== undefined) sql += ` LIMIT ${this.max}`;
    }
    return { sql, params };
  }
}
