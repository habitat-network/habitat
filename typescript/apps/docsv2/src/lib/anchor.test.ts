import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { relPosToString, stringToRelPos } from "./anchor";

describe("relative position serialization", () => {
  it("survives a JSON string round-trip and resolves to the same index", () => {
    const doc = new Y.Doc();
    const text = doc.getText("t");
    text.insert(0, "hello world");

    const rel = Y.createRelativePositionFromTypeIndex(text, 6);
    const restored = stringToRelPos(relPosToString(rel));
    const abs = Y.createAbsolutePositionFromRelativePosition(restored, doc);

    expect(abs?.index).toBe(6);
  });

  it("keeps pointing at the same content after an earlier insert", () => {
    const doc = new Y.Doc();
    const text = doc.getText("t");
    text.insert(0, "world");

    const rel = Y.createRelativePositionFromTypeIndex(text, 5);
    const s = relPosToString(rel);

    text.insert(0, "hello "); // shifts "world" right by 6
    const abs = Y.createAbsolutePositionFromRelativePosition(
      stringToRelPos(s),
      doc,
    );

    expect(abs?.index).toBe(11);
  });
});
