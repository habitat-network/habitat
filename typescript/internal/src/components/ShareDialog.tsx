import { Dialog, DialogContent, DialogTrigger, DialogTitle } from "./ui/dialog";
import UserCombobox from "./UserCombobox";
import { useState } from "react";
import { Actor } from "@/types/Actor";
import { AuthManager } from "@/authManager";
import { Button } from "./ui/button";
import { UserItem } from "./UserItem";
import { Spinner } from "./ui/spinner";
import { XIcon, GlobeIcon, CopyIcon, CheckIcon } from "lucide-react";

interface ShareDialogProps {
  grantees: Actor[];
  onAddPermission: (grantees: Actor[]) => void;
  onRemovePermission: (grantee: Actor) => void;
  authManager: AuthManager;
  isAdding?: boolean;
  isPublic?: boolean;
  publicUrl?: string;
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
  publicUrl,
  isMakingPublic,
  onMakePublic,
}: ShareDialogProps) => {
  const [newGrantees, setNewGrantees] = useState<Actor[]>([]);
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    if (!publicUrl) return;
    navigator.clipboard.writeText(publicUrl).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <Dialog>
      <DialogTrigger render={<Button>Share</Button>} />
      <DialogContent>
        <DialogTitle>Share</DialogTitle>
        {isPublic ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <GlobeIcon className="size-4 shrink-0" />
              <span>This document is public — anyone can view it.</span>
            </div>
            {publicUrl && (
              <div className="flex items-center gap-2">
                <input
                  readOnly
                  value={publicUrl}
                  className="flex-1 truncate rounded border px-2 py-1 text-sm font-mono bg-muted"
                />
                <Button variant="outline" size="icon" onClick={handleCopy} aria-label="Copy link">
                  {copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
                </Button>
              </div>
            )}
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
