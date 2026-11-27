import { describe, expect, it } from "vitest";
import { ACTION_SCOPES, permissionsFromRecord } from "api";

describe("permissionsFromRecord", () => {
  it("binds a role to every action its scopes cover", () => {
    const { bindings, assignable } = permissionsFromRecord({
      grants: [
        {
          role: "admin",
          scopes: [
            ...ACTION_SCOPES["community.configure"],
            "space:*?manage=create&manage=delete",
          ],
          assignable: ["admin", "member"],
        },
        {
          role: "staff",
          scopes: ["include:org.atgrp.manageMembers"],
          assignable: [],
        },
      ],
    });
    const rolesFor = (action: string) =>
      bindings.find((b) => b.action === action)?.roles;
    expect(rolesFor("community.configure")).toEqual(["admin"]);
    expect(rolesFor("space.create")).toEqual(["admin"]);
    expect(rolesFor("invite")).toEqual(["staff"]);
    expect(rolesFor("mcp.configure")).toBeUndefined();
    expect(assignable).toEqual([{ role: "admin", roles: ["admin", "member"] }]);
  });

  it("returns nothing for a missing record", () => {
    expect(permissionsFromRecord(undefined)).toEqual({
      bindings: [],
      assignable: [],
    });
  });
});
