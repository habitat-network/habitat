import { runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import type { DocRoom } from "../src/server/docs/rooms/docRoom";

// holdAlarm parks a room's real alarm an hour out, so it cannot fire in the
// background (during another test's wait, or mid-flush when a test reads its
// results) and only runs when runAlarmNow drives it. Call it right after the
// edits that schedule the room.
async function holdAlarm(stub: DurableObjectStub<DocRoom>): Promise<void> {
  await runInDurableObject(stub, (_r: DocRoom, state) =>
    state.storage.setAlarm(Date.now() + 60 * 60 * 1000),
  );
}

// runAlarmNow makes every pending flush due and runs the room's alarm to
// completion, with no real waiting. runDurableObjectAlarm runs the handler
// unconditionally but alarm() skips entries whose deadline hasn't passed, and
// workerd's Date.now() only advances on I/O, so rewinding the stored
// deadlines is what makes "the debounce elapsed" deterministic. It resolves
// only once the handler has finished (or rejects if the flush threw), so
// callers can assert straight away instead of polling.
export async function runAlarmNow(
  stub: DurableObjectStub<DocRoom>,
): Promise<void> {
  await holdAlarm(stub);
  await runInDurableObject(stub, async (_r: DocRoom, state) => {
    const pending = await state.storage.list<{
      idleDeadline: number;
      firstPushAt: number;
    }>({ prefix: "pending:" });
    for (const [key, entry] of pending) {
      await state.storage.put(key, {
        ...entry,
        idleDeadline: 0,
        firstPushAt: 0,
      });
    }
  });
  await runDurableObjectAlarm(stub);
}

// cancelAlarm stops a room's alarm for good, for tests that leave a flush
// pending on purpose.
export async function cancelAlarm(
  stub: DurableObjectStub<DocRoom>,
): Promise<void> {
  await runInDurableObject(stub, (_r: DocRoom, state) =>
    state.storage.deleteAlarm(),
  );
}
