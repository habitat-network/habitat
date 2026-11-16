import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { UserItem } from "./UserItem";

const did = "did:web:hello.test";

afterEach(cleanup);

describe("UserItem", () => {
  it("shows display name and handle", () => {
    render(
      <UserItem actor={{ did, displayName: "Alice", handle: "a.test" }} />,
    );
    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.getByText("@a.test")).toBeTruthy();
  });

  it("uses the handle as title without a display name", () => {
    render(<UserItem actor={{ did, handle: "a.test" }} />);
    expect(screen.getByText("a.test")).toBeTruthy();
    expect(screen.queryByText("@a.test")).toBeNull();
  });

  it("uses the DID when there is no profile", () => {
    render(<UserItem actor={{ did }} />);
    expect(screen.getByText(did)).toBeTruthy();
  });
});
