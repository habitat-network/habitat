import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import {
  updatePermissions,
  type ActionBinding,
  type AssignableBinding,
  type RoleView,
} from "@/queries/opensocial";
import { RoleCombobox } from "@/components/RoleCombobox";
import { OPENSOCIAL_ACTIONS } from "@/lib/opensocialActions";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Field,
  FieldLabel,
} from "internal/components/ui";

// rolesForAction/rolesForAssignable resolve the current binding for one
// action/role out of the flat bindings/assignable arrays into the RoleView
// objects the combobox works with.
function rolesForAction(
  bindings: ActionBinding[],
  action: string,
  roles: RoleView[],
): RoleView[] {
  const rkeys = bindings.find((b) => b.action === action)?.roles ?? [];
  return roles.filter((r) => rkeys.includes(r.rkey));
}

function rolesForAssignable(
  assignable: AssignableBinding[],
  role: string,
  roles: RoleView[],
): RoleView[] {
  const rkeys = assignable.find((a) => a.role === role)?.roles ?? [];
  return roles.filter((r) => rkeys.includes(r.rkey));
}

function withAction(
  bindings: ActionBinding[],
  action: string,
  selected: RoleView[],
): ActionBinding[] {
  const rest = bindings.filter((b) => b.action !== action);
  const roles = selected.map((r) => r.rkey);
  return roles.length > 0 ? [...rest, { action, roles }] : rest;
}

function withAssignable(
  assignable: AssignableBinding[],
  role: string,
  selected: RoleView[],
): AssignableBinding[] {
  const rest = assignable.filter((a) => a.role !== role);
  const roles = selected.map((r) => r.rkey);
  return roles.length > 0 ? [...rest, { role, roles }] : rest;
}

export function CapabilitiesEditor({
  org,
  roles,
  bindings,
  assignable,
  authManager,
}: {
  org: string;
  roles: RoleView[];
  bindings: ActionBinding[];
  assignable: AssignableBinding[];
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();

  const { mutate: save, isPending: saving } = useMutation({
    mutationFn: (next: {
      bindings: ActionBinding[];
      assignable: AssignableBinding[];
    }) => updatePermissions(authManager, org, next.bindings, next.assignable),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ["opensocial", "permissions", org],
      }),
  });

  return (
    <div className="flex flex-col gap-6">
      <Card size="sm">
        <CardHeader>
          <CardTitle className="text-base">Capabilities</CardTitle>
          <p className="text-sm text-muted-foreground">
            Which roles authorize each action. A member's authorized actions are
            the union of every role they hold, so an action can be bound to
            several roles at once.
          </p>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {OPENSOCIAL_ACTIONS.map(({ action, label, hint }) => (
            <Field key={action}>
              <FieldLabel>{label}</FieldLabel>
              <p className="text-xs text-muted-foreground -mt-1">{hint}</p>
              <RoleCombobox
                roles={roles}
                value={rolesForAction(bindings, action, roles)}
                onValueChange={(next) =>
                  save({
                    bindings: withAction(bindings, action, next),
                    assignable,
                  })
                }
                disabled={saving}
              />
            </Field>
          ))}
        </CardContent>
      </Card>

      <Card size="sm">
        <CardHeader>
          <CardTitle className="text-base">Assignable roles</CardTitle>
          <p className="text-sm text-muted-foreground">
            Which roles a holder of each role may grant, revoke, or eject a
            holder of (via the assign-roles and eject actions above).
          </p>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {roles.map((role) => (
            <Field key={role.rkey}>
              <FieldLabel>{role.name} may assign/eject</FieldLabel>
              <RoleCombobox
                roles={roles}
                value={rolesForAssignable(assignable, role.rkey, roles)}
                onValueChange={(next) =>
                  save({
                    bindings,
                    assignable: withAssignable(assignable, role.rkey, next),
                  })
                }
                disabled={saving}
              />
            </Field>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
