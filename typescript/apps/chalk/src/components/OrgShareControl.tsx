import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
  toast,
} from "internal/components/ui";
import { UsersIcon } from "lucide-react";
import {
  getDocOrgAccess,
  revokeDocOrgAccess,
  shareDocWithOrg,
} from "@/server/functions";

type OrgAccess = "editor" | "viewer" | "none";

const LABEL: Record<OrgAccess, string> = {
  none: "Not shared with org",
  viewer: "Org can view",
  editor: "Org can edit",
};

// OrgShareControl lets a doc's editor grant (or revoke) access to every
// member of the current org at once, via a single spaceRelation naming the
// org's own members space as its subject — the org-mode counterpart to
// ShareDialog's per-person sharing.
export function OrgShareControl({ docId }: { docId: string }) {
  const queryClient = useQueryClient();
  const queryKey = ["docOrgAccess", docId];

  const { data: access = "none" } = useQuery({
    queryKey,
    queryFn: async (): Promise<OrgAccess> =>
      (await getDocOrgAccess({ data: { docId } })) ?? "none",
  });

  const { mutate: setAccess, isPending } = useMutation({
    mutationFn: (next: OrgAccess) =>
      next === "none"
        ? revokeDocOrgAccess({ data: { docId } })
        : shareDocWithOrg({ data: { docId, role: next } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
    onError: (error) => {
      toast.add({
        type: "error",
        title: "Couldn't update org access",
        description: error.message,
      });
    },
  });

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="outline" disabled={isPending}>
            <UsersIcon className="size-4" />
            {LABEL[access]}
          </Button>
        }
      />
      <DropdownMenuContent>
        <DropdownMenuRadioGroup
          value={access}
          onValueChange={(value) => setAccess(value as OrgAccess)}
        >
          <DropdownMenuRadioItem value="none">
            {LABEL.none}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="viewer">
            {LABEL.viewer}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="editor">
            {LABEL.editor}
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
