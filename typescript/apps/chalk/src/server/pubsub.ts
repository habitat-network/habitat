import type * as Y from "yjs";

type Listener = (doc: Y.Doc) => void;

// DocPubSub fans out merged Y.Doc snapshots to every subscriber of a given
// space, in-process. One process serves every connected client (chalk is a
// single Node server), so this needs no external broker.
export class DocPubSub {
  private listeners = new Map<string, Set<Listener>>();

  publish(spaceUri: string, ydoc: Y.Doc): void {
    for (const listener of this.listeners.get(spaceUri) ?? []) {
      listener(ydoc);
    }
  }

  async *subscribe(spaceUri: string): AsyncIterable<Y.Doc> {
    const queue: Y.Doc[] = [];
    let wake: (() => void) | undefined;
    const listener: Listener = (doc) => {
      queue.push(doc);
      wake?.();
    };
    let set = this.listeners.get(spaceUri);
    if (!set) {
      set = new Set();
      this.listeners.set(spaceUri, set);
    }
    set.add(listener);
    try {
      while (true) {
        if (queue.length === 0) {
          await new Promise<void>((resolve) => {
            wake = resolve;
          });
          wake = undefined;
        }
        while (queue.length > 0) {
          yield queue.shift()!;
        }
      }
    } finally {
      set.delete(listener);
      if (set.size === 0) this.listeners.delete(spaceUri);
    }
  }
}
