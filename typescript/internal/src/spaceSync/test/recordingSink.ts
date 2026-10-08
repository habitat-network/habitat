import { Effect, Layer, Stream } from "effect";
import { SinkError } from "../errors";
import { SyncSink } from "../SyncSink";

export interface RecordedBatch {
  readonly _tag: string;
  readonly space: string;
  readonly did?: string;
  readonly rev?: string;
  readonly paths?: ReadonlyArray<string>;
}

export interface RecordingSinkState {
  readonly batches: RecordedBatch[];
  /** space|did → path → cid string: the materialized view a real index would hold. */
  readonly views: Map<string, Map<string, string>>;
  view(space: string, did: string): Map<string, string>;
  failNext: number;
  applyDelayMs: number;
  readonly inFlight: Set<string>;
  maxConcurrentSpaces: number;
  interrupted: number;
}

export const makeRecordingSink = () => {
  const state: RecordingSinkState = {
    batches: [],
    views: new Map(),
    view: (space, did) => state.views.get(`${space}|${did}`) ?? new Map(),
    failNext: 0,
    applyDelayMs: 0,
    inFlight: new Set(),
    maxConcurrentSpaces: 0,
    interrupted: 0,
  };

  const layer = Layer.succeed(
    SyncSink,
    SyncSink.of({
      apply: (batch) =>
        Effect.gen(function* () {
          state.inFlight.add(batch.space);
          state.maxConcurrentSpaces = Math.max(state.maxConcurrentSpaces, state.inFlight.size);
          if (state.applyDelayMs > 0) yield* Effect.sleep(state.applyDelayMs);
          if (state.failNext > 0) {
            state.failNext--;
            return yield* new SinkError({ message: "sink failure (test)", cause: new Error("boom") });
          }
          switch (batch._tag) {
            case "Ops": {
              const key = `${batch.space}|${batch.did}`;
              const view = new Map(state.views.get(key));
              for (const change of batch.changes) {
                const path = `${change.collection}/${change.rkey}`;
                if (change.cid) view.set(path, change.cid.toString());
                else view.delete(path);
              }
              state.views.set(key, view);
              state.batches.push({ _tag: "Ops", space: batch.space, did: batch.did, rev: batch.rev, paths: batch.changes.map((c) => `${c.collection}/${c.rkey}`) });
              return;
            }
            case "Reset": {
              const records = yield* Stream.runCollect(batch.records).pipe(
                Effect.mapError((e) => new SinkError({ message: e.message, cause: e })),
              );
              state.views.set(`${batch.space}|${batch.did}`, new Map(records.map((r) => [`${r.collection}/${r.rkey}`, r.cid.toString()])));
              state.batches.push({ _tag: "Reset", space: batch.space, did: batch.did, rev: batch.rev, paths: records.map((r) => `${r.collection}/${r.rkey}`) });
              return;
            }
            case "RepoRemoved":
              state.views.delete(`${batch.space}|${batch.did}`);
              state.batches.push({ _tag: "RepoRemoved", space: batch.space, did: batch.did });
              return;
            case "SpaceDeleted":
              for (const key of [...state.views.keys()]) if (key.startsWith(`${batch.space}|`)) state.views.delete(key);
              state.batches.push({ _tag: "SpaceDeleted", space: batch.space });
              return;
          }
        }).pipe(
          Effect.onInterrupt(() => Effect.sync(() => void state.interrupted++)),
          Effect.ensuring(Effect.sync(() => void state.inFlight.delete(batch.space))),
        ),
    }),
  );

  return { state, layer };
};