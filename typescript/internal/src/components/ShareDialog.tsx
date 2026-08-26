import { Dialog, DialogContent, DialogTrigger, DialogTitle } from "./ui/dialog";
import UserCombobox from "./UserCombobox";
import { useState } from "react";
import { Actor } from "@/types/Actor";
import { Button } from "./ui/button";
import { ButtonGroup } from "./ui/button-group";
import { UserAvatar } from "./UserAvatar";
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

export type Role = "editor" | "viewer";

export interface Grantee extends Actor {
  relation?: "writer" | "reader";
}

const RELATION_LABEL: Record<"writer" | "reader", string> = {
  writer: "Editor",
  reader: "Viewer",
};

interface ShareDialogProps {
  grantees: Grantee[];
  onAddPermission: (grantees: Actor[], role: Role) => void;
  onRemovePermission: (grantee: Actor) => void;
  isAdding?: boolean;
  // Shows an editor/viewer role picker for new grantees and a role column
  // for existing ones. Off by default: not every caller (e.g. docs' clique-
  // based sharing) has a reader/writer distinction.
  roles?: boolean;
  // The signed-in user's own DID. When a grantee's did matches, its remove
  // button is hidden — a user shouldn't be able to revoke their own access
  // from the share modal.
  currentUserDid?: string;
}

const ShareDialog = ({
  grantees,
  isAdding,
  onAddPermission,
  onRemovePermission,
  roles = false,
  currentUserDid,
}: ShareDialogProps) => {
  const [newGrantees, setNewGrantees] = useState<Actor[]>([]);
  const [role, setRole] = useState<Role>("editor");

  return (
    <Dialog>
      <DialogTrigger render={<Button>Share</Button>} />
      <DialogContent>
        <DialogTitle>Share</DialogTitle>
        <div className="flex gap-2">
          <div className="flex-1">
            <UserCombobox value={newGrantees} onValueChange={setNewGrantees} />
          </div>
          {roles && (
            <ButtonGroup>
              <Button
                type="button"
                variant={role === "editor" ? "default" : "outline"}
                onClick={() => setRole("editor")}
              >
                Editor
              </Button>
              <Button
                type="button"
                variant={role === "viewer" ? "default" : "outline"}
                onClick={() => setRole("viewer")}
              >
                Viewer
              </Button>
            </ButtonGroup>
          )}
        </div>
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
                    {g.displayName ||
                      (g.handle ? `@${g.handle}` : "Unknown User")}
                  </div>
                </TableCell>
                {roles && (
                  <TableCell>
                    {g.relation ? RELATION_LABEL[g.relation] : ""}
                  </TableCell>
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
      </DialogContent>
    </Dialog>
  );
};

export default ShareDialog;
