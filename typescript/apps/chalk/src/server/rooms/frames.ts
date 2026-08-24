// A streamed Response body has no message boundaries, so each Yjs update is
// written as a 4-byte big-endian length followed by that many bytes.
export function frame(bytes: Uint8Array): Uint8Array {
  const out = new Uint8Array(4 + bytes.byteLength);
  new DataView(out.buffer).setUint32(0, bytes.byteLength, false);
  out.set(bytes, 4);
  return out;
}

export async function* readFrames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<Uint8Array> {
  const reader = body.getReader();
  let buf = new Uint8Array(0);
  while (true) {
    while (buf.byteLength >= 4) {
      const len = new DataView(buf.buffer, buf.byteOffset, 4).getUint32(
        0,
        false,
      );
      if (buf.byteLength < 4 + len) break;
      yield buf.slice(4, 4 + len);
      buf = buf.slice(4 + len);
    }
    const { done, value } = await reader.read();
    if (done) return;
    const merged = new Uint8Array(buf.byteLength + value.byteLength);
    merged.set(buf);
    merged.set(value, buf.byteLength);
    buf = merged;
  }
}
