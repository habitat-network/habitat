import { Dialog, DialogContent, DialogTrigger, DialogTitle } from "./ui/dialog";
import UserCombobox from "./UserCombobox";
import { useState, type ReactNode } from "react";
import { Actor } from "@/types/Actor";
import { Button } from "./ui/button";
import { ButtonGroup } from "./ui/button-group";
import { UserAvatar } from "./UserAvatar";
import { UserDisplayName } from "./UserDisplayName";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import { Spinner } from "./ui/spinner";
import { XIcon } from "lucide-react";

// Role is what a grantee may do, in descending order of privilege. A
// commenter sits between the two: they can't change the document itself,
// but unlike a viewer they can add comments to it. Consumers map these
// onto whatever relations they actually store — the dialog only ever
// speaks in roles.
export type Role = "editor" | "commenter" | "viewer";

// ROLES drives both the picker and the labels, so a role can't be offered
// without being renderable in the table (or the reverse).
const ROLES: { role: Role; label: string }[] = [
  { role: "editor", label: "Editor" },
  { role: "commenter", label: "Commenter" },
  { role: "viewer", label: "Viewer" },
];

const ROLE_LABEL: Record<Role, string> = Object.fromEntries(
  ROLES.map((r) => [r.role, r.label]),
) as Record<Role, string>;

export interface Grantee extends Actor {
  role?: Role;
}

interface ShareDialogProps {
  grantees: Grantee[];
  onAddPermission: (grantees: Actor[], role: Role) => void;
  onRemovePermission: (grantee: Actor) => void;
  isAdding?: boolean;
  // Shows a role picker for new grantees and a role column for existing
  // ones. Off by default: not every caller (e.g. docs' clique-based
  // sharing) has a role distinction at all.
  roles?: boolean;
  // The signed-in user's own DID. When a grantee's did matches, its remove
  // button is hidden — a user shouldn't be able to revoke their own access
  // from the share modal.
  currentUserDid?: string;
  // Extra access controls rendered below the grantee list, for access that
  // isn't a per-person grant — chalk passes its org-wide share control here.
  children?: ReactNode;
}

const ShareDialog = ({
  grantees,
  isAdding,
  onAddPermission,
  onRemovePermission,
  roles = false,
  currentUserDid,
  children,
}: ShareDialogProps) => {
  const [newGrantees, setNewGrantees] = useState<Actor[]>([]);
  const [role, setRole] = useState<Role>("editor");

  return (
    <Dialog>
      <DialogTrigger render={<Button>Share</Button>} />
      <DialogContent>
        <DialogTitle>Share</DialogTitle>
        <UserCombobox value={newGrantees} onValueChange={setNewGrantees} />
        {roles && (
          <ButtonGroup>
            {ROLES.map(({ role: r, label }) => (
              <Button
                key={r}
                type="button"
                variant={role === r ? "default" : "outline"}
                onClick={() => setRole(r)}
              >
                {label}
              </Button>
            ))}
          </ButtonGroup>
        )}
        <Button
          onClick={() => {
            onAddPermission(newGrantees, role);
            setNewGrantees([]);
          }}
          disabled={isAdding}
        >
          {isAdding && <Spinner />}
          Add
        </Button>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Person</TableHead>
              {roles && <TableHead>Role</TableHead>}
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {grantees.map((g) => (
              <TableRow key={g.did}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <UserAvatar actor={g} size="sm" />
                    <UserDisplayName actor={g} />
                  </div>
                </TableCell>
                {roles && (
                  <TableCell>{g.role ? ROLE_LABEL[g.role] : ""}</TableCell>
                )}
                <TableCell>
                  {g.did !== currentUserDid && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove ${g.handle}`}
                      onClick={() => onRemovePermission(g)}
                    >
                      <XIcon />
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {children}
      </DialogContent>
    </Dialog>
  );
};

export default ShareDialog;
