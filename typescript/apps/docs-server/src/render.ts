import * as Y from "yjs";

export interface RenderedDoc {
  title: string;
  markdown: string;
}

// renderDoc converts a TipTap/Yjs document (the "default" XML fragment) into
// markdown and derives a title. It walks the Yjs types directly to avoid
// pulling a ProseMirror/TipTap schema into the server.
export function renderDoc(ydoc: Y.Doc): RenderedDoc {
  const fragment = ydoc.getXmlFragment("default");
  const blocks: string[] = [];
  let title = "";

  for (const node of fragment.toArray()) {
    if (!(node instanceof Y.XmlElement)) {
      continue;
    }
    const md = renderBlock(node);
    if (md.length) {
      blocks.push(md);
    }
    if (!title && node.nodeName === "heading") {
      const t = inline(node).trim();
      if (t) {
        title = t;
      }
    }
  }

  if (!title) {
    // Fall back to the first non-empty block's first line.
    for (const b of blocks) {
      const t = b.replace(/^#+\s*/, "").trim();
      if (t) {
        title = t.split("\n")[0];
        break;
      }
    }
  }

  return { title: title || "Untitled", markdown: blocks.join("\n\n") };
}

function renderBlock(el: Y.XmlElement, depth = 0): string {
  switch (el.nodeName) {
    case "heading": {
      const level = clampLevel(el.getAttribute("level"));
      return `${"#".repeat(level)} ${inline(el)}`;
    }
    case "paragraph":
      return inline(el);
    case "bulletList":
      return renderList(el, "bullet", depth);
    case "orderedList":
      return renderList(el, "ordered", depth);
    case "blockquote":
      return childBlocks(el, depth)
        .join("\n")
        .split("\n")
        .map((l) => `> ${l}`)
        .join("\n");
    case "codeBlock":
      return "```\n" + plainText(el) + "\n```";
    case "horizontalRule":
      return "---";
    default:
      return inline(el);
  }
}

function renderList(
  el: Y.XmlElement,
  kind: "bullet" | "ordered",
  depth: number,
): string {
  const lines: string[] = [];
  const indent = "  ".repeat(depth);
  let i = 1;
  for (const item of el.toArray()) {
    if (!(item instanceof Y.XmlElement) || item.nodeName !== "listItem") {
      continue;
    }
    const marker = kind === "bullet" ? "-" : `${i++}.`;
    const parts: string[] = [];
    for (const child of item.toArray()) {
      if (!(child instanceof Y.XmlElement)) {
        continue;
      }
      if (child.nodeName === "bulletList" || child.nodeName === "orderedList") {
        parts.push(renderBlock(child, depth + 1));
      } else {
        parts.push(inline(child));
      }
    }
    const [first, ...rest] = parts;
    lines.push(`${indent}${marker} ${first ?? ""}`);
    for (const r of rest) {
      lines.push(r);
    }
  }
  return lines.join("\n");
}

function childBlocks(el: Y.XmlElement, depth: number): string[] {
  const out: string[] = [];
  for (const child of el.toArray()) {
    if (child instanceof Y.XmlElement) {
      out.push(renderBlock(child, depth));
    }
  }
  return out;
}

// inline renders the inline content of a block (text with marks, hard breaks).
function inline(el: Y.XmlElement): string {
  let out = "";
  for (const child of el.toArray()) {
    if (child instanceof Y.XmlText) {
      out += renderText(child);
    } else if (child instanceof Y.XmlElement) {
      out += child.nodeName === "hardBreak" ? "  \n" : inline(child);
    }
  }
  return out;
}

interface DeltaAttributes {
  bold?: unknown;
  italic?: unknown;
  code?: unknown;
  strike?: unknown;
  link?: { href?: string };
}

function renderText(text: Y.XmlText): string {
  let out = "";
  for (const op of text.toDelta() as { insert?: unknown; attributes?: DeltaAttributes }[]) {
    if (typeof op.insert !== "string") {
      continue;
    }
    let s = op.insert;
    const a = op.attributes ?? {};
    if (a.code) s = "`" + s + "`";
    if (a.bold) s = "**" + s + "**";
    if (a.italic) s = "*" + s + "*";
    if (a.strike) s = "~~" + s + "~~";
    if (a.link?.href) s = `[${s}](${a.link.href})`;
    out += s;
  }
  return out;
}

function plainText(el: Y.XmlElement): string {
  let out = "";
  for (const child of el.toArray()) {
    if (child instanceof Y.XmlText) {
      out += (child.toDelta() as { insert?: unknown }[])
        .map((op) => (typeof op.insert === "string" ? op.insert : ""))
        .join("");
    } else if (child instanceof Y.XmlElement) {
      out += plainText(child);
    }
  }
  return out;
}

function clampLevel(level: unknown): number {
  const n = typeof level === "number" ? level : parseInt(String(level ?? 1), 10);
  if (Number.isNaN(n)) {
    return 1;
  }
  return Math.min(6, Math.max(1, n));
}
