import { describe, expect, it } from "vitest";
import { assignableWith } from "@/server/groups";

describe("assignableWith", () => {
  const bindings = [
    { action: "role.assign", roles: ["admin"] },
    { action: "invite", roles: ["admin", "mod"] },
  ];

  it("adds a new role to every role holding role.assign", () => {
    expect(
      assignableWith(
        bindings,
        [{ role: "admin", roles: ["admin", "member"] }],
        "mo-qa-role",
      ),
    ).toEqual([{ role: "admin", roles: ["admin", "member", "mo-qa-role"] }]);
  });

  it("starts an entry for an assigner that had none", () => {
    expect(assignableWith(bindings, [], "r")).toEqual([
      { role: "admin", roles: ["r"] },
    ]);
  });

  it("answers null when nothing needs to change", () => {
    expect(
      assignableWith(bindings, [{ role: "admin", roles: ["r"] }], "r"),
    ).toBeNull();
    expect(assignableWith([], [], "r")).toBeNull();
  });
});
