import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { CommentsSidebar } from "./CommentsSidebar";
import type { CommentThread } from "@/queries/comments";

const threads: CommentThread[] = [
  {
    uri: "ats://org/network.habitat.docs.comments/doc1/did:web:alice/network.habitat.docs.comment/c1",
    author: "did:web:alice",
    body: "top level",
    createdAt: "2026-07-07T00:00:00.000Z",
    range: { start: "s", end: "e" },
    replies: [
      {
        uri: "ats://org/network.habitat.docs.comments/doc1/did:web:bob/network.habitat.docs.comment/r1",
        author: "did:web:bob",
        body: "a reply",
        createdAt: "2026-07-07T00:00:01.000Z",
      },
    ],
  },
];

afterEach(cleanup);

describe("CommentsSidebar", () => {
  it("renders comments and replies", () => {
    render(
      <CommentsSidebar
        threads={threads}
        canComment
        onSelect={() => {}}
        onReply={() => {}}
        isReplying={null}
      />,
    );
    expect(screen.getByText("top level")).toBeTruthy();
    expect(screen.getByText("a reply")).toBeTruthy();
  });

  it("calls onSelect with the thread when a comment is clicked", () => {
    const onSelect = vi.fn();
    render(
      <CommentsSidebar
        threads={threads}
        canComment
        onSelect={onSelect}
        onReply={() => {}}
        isReplying={null}
      />,
    );
    fireEvent.click(screen.getByText("top level"));
    expect(onSelect).toHaveBeenCalledWith(threads[0]);
  });
});
