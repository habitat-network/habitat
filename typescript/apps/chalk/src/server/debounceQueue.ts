interface Pending<V> {
  value: V;
  idleTimer: ReturnType<typeof setTimeout>;
  maxWaitTimer: ReturnType<typeof setTimeout>;
}

export interface DebounceQueueOptions<K, V> {
  idleMs: number;
  maxWaitMs: number;
  flush: (key: K, value: V) => Promise<void>;
  merge: (existing: V | undefined, incoming: V) => V;
}

// DebounceQueue accumulates values per key and calls flush once activity for
// that key goes idle (idleMs since the last push) or maxWaitMs has elapsed
// since the first unflushed push, whichever comes first.
export class DebounceQueue<K, V> {
  private pending = new Map<K, Pending<V>>();

  constructor(private opts: DebounceQueueOptions<K, V>) {}

  push(key: K, value: V): void {
    const existing = this.pending.get(key);
    if (existing) {
      clearTimeout(existing.idleTimer);
      existing.value = this.opts.merge(existing.value, value);
      existing.idleTimer = setTimeout(() => this.flush(key), this.opts.idleMs);
      return;
    }
    const entry: Pending<V> = {
      value: this.opts.merge(undefined, value),
      idleTimer: setTimeout(() => this.flush(key), this.opts.idleMs),
      maxWaitTimer: setTimeout(() => this.flush(key), this.opts.maxWaitMs),
    };
    this.pending.set(key, entry);
  }

  private flush(key: K): void {
    const entry = this.pending.get(key);
    if (!entry) return;
    clearTimeout(entry.idleTimer);
    clearTimeout(entry.maxWaitTimer);
    this.pending.delete(key);
    void this.opts.flush(key, entry.value);
  }

  async flushAll(): Promise<void> {
    const keys = [...this.pending.keys()];
    const flushes: Promise<void>[] = [];
    for (const key of keys) {
      const entry = this.pending.get(key);
      if (!entry) continue;
      clearTimeout(entry.idleTimer);
      clearTimeout(entry.maxWaitTimer);
      this.pending.delete(key);
      flushes.push(this.opts.flush(key, entry.value));
    }
    await Promise.all(flushes);
  }
}
