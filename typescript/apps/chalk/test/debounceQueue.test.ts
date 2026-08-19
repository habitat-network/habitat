import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DebounceQueue } from "../src/server/debounceQueue";

describe("DebounceQueue", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("flushes idleMs after the last push", async () => {
    const flush = vi.fn(async () => {});
    const queue = new DebounceQueue<string, number>({
      idleMs: 2000,
      maxWaitMs: 10000,
      flush,
      merge: (a, b) => (a ?? 0) + b,
    });

    queue.push("doc1", 1);
    await vi.advanceTimersByTimeAsync(1000);
    queue.push("doc1", 2); // resets the idle timer
    await vi.advanceTimersByTimeAsync(1999);
    expect(flush).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(flush).toHaveBeenCalledWith("doc1", 3);
  });

  it("flushes at maxWaitMs even under continuous activity", async () => {
    const flush = vi.fn(async () => {});
    const queue = new DebounceQueue<string, number>({
      idleMs: 2000,
      maxWaitMs: 5000,
      flush,
      merge: (a, b) => (a ?? 0) + b,
    });

    queue.push("doc1", 1);
    for (let i = 0; i < 4; i++) {
      await vi.advanceTimersByTimeAsync(1500); // keeps resetting the idle timer
      queue.push("doc1", 1);
    }
    // 4 * 1500ms = 6000ms elapsed, past maxWaitMs (5000ms), despite idle
    // never reaching 2000ms uninterrupted.
    expect(flush).toHaveBeenCalledWith("doc1", 4);
  });

  it("keys are independent", async () => {
    const flush = vi.fn(async () => {});
    const queue = new DebounceQueue<string, number>({
      idleMs: 100,
      maxWaitMs: 1000,
      flush,
      merge: (a, b) => (a ?? 0) + b,
    });
    queue.push("doc1", 1);
    queue.push("doc2", 2);
    await vi.advanceTimersByTimeAsync(100);
    expect(flush).toHaveBeenCalledWith("doc1", 1);
    expect(flush).toHaveBeenCalledWith("doc2", 2);
  });

  it("flushAll flushes every pending key immediately", async () => {
    const flush = vi.fn(async () => {});
    const queue = new DebounceQueue<string, number>({
      idleMs: 60000,
      maxWaitMs: 60000,
      flush,
      merge: (a, b) => (a ?? 0) + b,
    });
    queue.push("doc1", 1);
    queue.push("doc2", 2);
    await queue.flushAll();
    expect(flush).toHaveBeenCalledWith("doc1", 1);
    expect(flush).toHaveBeenCalledWith("doc2", 2);
  });
});
