import { describe, expect, it } from "vitest";
import { ACTION_SCOPES, permissionsFromRoles } from "api";

describe("permissionsFromRoles", () => {
  it("binds a role to every action its scopes cover", () => {
    const { bindings, assignable } = permissionsFromRoles([
      {
        rkey: "admin",
        value: {
          permissions: [
            ...ACTION_SCOPES["community.configure"],
            "space:*?manage=create&manage=delete",
          ],
          canAssign: ["admin", "member"],
        },
      },
      {
        rkey: "staff",
        value: { permissions: ["include:org.atgrp.authManageMembers"] },
      },
      { rkey: "bot", value: { permissions: ["habitat:action=mcp.configure"] } },
    ]);
    const rolesFor = (action: string) =>
      bindings.find((b) => b.action === action)?.roles;
    expect(rolesFor("community.configure")).toEqual(["admin"]);
    expect(rolesFor("space.create")).toEqual(["admin"]);
    expect(rolesFor("space.configure")).toBeUndefined();
    expect(rolesFor("invite")).toEqual(["staff"]);
    expect(rolesFor("mcp.configure")).toEqual(["bot"]);
    expect(assignable).toEqual([{ role: "admin", roles: ["admin", "member"] }]);
  });

  it("expands a wildcard canAssign to every role", () => {
    const { assignable } = permissionsFromRoles([
      { rkey: "admin", value: { canAssign: ["*"] } },
      { rkey: "member", value: {} },
    ]);
    expect(assignable).toEqual([{ role: "admin", roles: ["admin", "member"] }]);
  });

  it("returns nothing without role records", () => {
    expect(permissionsFromRoles([])).toEqual({ bindings: [], assignable: [] });
  });
});
