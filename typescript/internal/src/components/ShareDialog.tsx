import { Dialog, DialogContent, DialogTrigger, DialogTitle } from "./ui/dialog";
import UserCombobox from "./UserCombobox";
import { useState } from "react";
import { Actor } from "@/types/Actor";
import { AuthManager } from "@/authManager";
import { Button } from "./ui/button";
import { UserItem } from "./UserItem";
import { Spinner } from "./ui/spinner";
import { XIcon, GlobeIcon } from "lucide-react";

interface ShareDialogProps {
  grantees: Actor[];
  onAddPermission: (grantees: Actor[]) => void;
  onRemovePermission: (grantee: Actor) => void;
  authManager: AuthManager;
  isAdding?: boolean;
  isPublic?: boolean;
  isMakingPublic?: boolean;
  onMakePublic?: () => void;
}

const ShareDialog = ({
  grantees,
  authManager,
  isAdding,
  onAddPermission,
  onRemovePermission,
  isPublic,
  isMakingPublic,
  onMakePublic,
}: ShareDialogProps) => {
  const [newGrantees, setNewGrantees] = useState<Actor[]>([]);
  return (
    <Dialog>
      <DialogTrigger render={<Button>Share</Button>} />
      <DialogContent>
        <DialogTitle>Share</DialogTitle>
        {isPublic ? (
          <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
            <GlobeIcon className="size-4 shrink-0" />
            <span>This document is public — anyone can view it.</span>
          </div>
        ) : (
          <>
            {onMakePublic && (
              <Button
                variant="outline"
                disabled={isMakingPublic}
                onClick={onMakePublic}
              >
                {isMakingPublic ? <Spinner /> : <GlobeIcon className="size-4" />}
                Make public
              </Button>
            )}
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
            {grantees.map((g) => (
              <UserItem
                key={g.handle}
                actor={g}
                actions={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Remove ${g.handle}`}
                    onClick={() => onRemovePermission(g)}
                  >
                    <XIcon />
                  </Button>
                }
              />
            ))}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
};

export default ShareDialog;
