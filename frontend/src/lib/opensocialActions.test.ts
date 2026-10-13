import { describe, expect, it } from "vitest";
import { assignableRoles, canAssignRoles } from "./opensocialActions";

const declared = ["admin", "member", "editor"];
const bindings = [{ action: "role.assign", roles: ["editor"] }];
const assignable = [{ role: "editor", roles: ["member"] }];

describe("assignableRoles", () => {
  it("falls back to every declared role for admins without a config", () => {
    expect(assignableRoles([], [], ["admin"], declared)).toEqual(declared);
  });

  it("gives non-admins nothing without a config", () => {
    expect(assignableRoles([], [], ["member"], declared)).toEqual([]);
  });

  it("unions the assignable roles of every role the user holds", () => {
    const a = [
      { role: "editor", roles: ["member"] },
      { role: "member", roles: ["editor"] },
    ];
    expect(
      assignableRoles(bindings, a, ["editor", "member"], declared).sort(),
    ).toEqual(["editor", "member"]);
  });
});

describe("canAssignRoles", () => {
  it("allows a non-admin the config permits", () => {
    expect(canAssignRoles(bindings, assignable, ["editor"], declared)).toBe(
      true,
    );
  });

  it("denies an admin the config does not bind to role.assign", () => {
    expect(canAssignRoles(bindings, assignable, ["admin"], declared)).toBe(
      false,
    );
  });

  it("denies a role.assign holder with no assignable roles", () => {
    expect(canAssignRoles(bindings, [], ["editor"], declared)).toBe(false);
  });

  it("allows admins when no config exists", () => {
    expect(canAssignRoles([], [], ["admin"], declared)).toBe(true);
  });
});
