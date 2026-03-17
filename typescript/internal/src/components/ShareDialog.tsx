import { Dialog, DialogContent, DialogTrigger, DialogTitle } from "./ui/dialog";
import UserCombobox from "./UserCombobox";
import { useState } from "react";
import { Actor } from "@/types/Actor";
import { AuthManager } from "@/authManager";
import { Button } from "./ui/button";
import { UserItem } from "./UserItem";
import { Spinner } from "./ui/spinner";

interface ShareDialogProps {
  grantees: Actor[];
  onAddPermission: (grantees: Actor[]) => void;
  authManager: AuthManager;
  isAdding?: boolean;
}
const ShareDialog = ({
  grantees,
  authManager,
  isAdding,
  onAddPermission,
}: ShareDialogProps) => {
  const [newGrantees, setNewGrantees] = useState<Actor[]>([]);
  return (
    <Dialog>
      <DialogTrigger render={<Button>Share</Button>} />
      <DialogContent>
        <DialogTitle>Share</DialogTitle>
        <UserCombobox
          value={newGrantees}
          onValueChange={setNewGrantees}
          authManager={authManager}
        />
        <Button
          onClick={() => {
            onAddPermission(newGrantees);
            setNewGrantees([]);
          }}
          disabled={isAdding}
        >
          {isAdding && <Spinner />}
          Add
        </Button>
        {grantees.map((g) => {
          return <UserItem key={g.handle} actor={g} />;
        })}
      </DialogContent>
    </Dialog>
  );
};

export default ShareDialog;
