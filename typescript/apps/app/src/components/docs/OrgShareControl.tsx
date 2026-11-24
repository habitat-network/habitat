import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  SelectGroup,
  toast,
} from "internal/components/ui";
import { UsersIcon } from "lucide-react";
import { docOrgAccessQuery } from "@/lib/queries/docs";
import { revokeDocOrgAccess, shareDocWithOrg } from "@/server/fns/docs";

type OrgAccess = "editor" | "viewer" | "none";

const LABEL: Record<OrgAccess, string> = {
  none: "No access",
  viewer: "Can view",
  editor: "Can edit",
};

// OrgShareControl lets a doc's editor grant (or revoke) access to every
// member of the current org at once, via a single spaceRelation naming the
// org's own members space as its subject. Rendered inside ShareDialog (as
// its children) alongside the per-person grants it complements.
export function OrgShareControl({
  org,
  docId,
  orgName,
}: {
  org: string;
  docId: string;
  orgName: string;
}) {
  const queryClient = useQueryClient();
  const { queryKey } = docOrgAccessQuery(org, docId);

  const { data: access } = useSuspenseQuery(docOrgAccessQuery(org, docId));

  const { mutate: setAccess, isPending } = useMutation({
    mutationFn: (next: OrgAccess) =>
      next === "none"
        ? revokeDocOrgAccess({ data: { org, docId } })
        : shareDocWithOrg({ data: { org, docId, role: next } }),
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
    // px-3 matches the grantee table's cell padding, so the header and row
    // line up with the per-person grants listed above them.
    <div className="flex flex-col gap-2 px-3">
      <h3 className="font-medium">Organization-wide permissions</h3>
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-sm">
          <UsersIcon className="size-4 text-muted-foreground" />
          <span>Everyone at {orgName}</span>
        </div>
        <Select
          value={access}
          onValueChange={(next) => setAccess(next as OrgAccess)}
          disabled={isPending}
        >
          <SelectTrigger
            size="sm"
            aria-label={`Access for everyone at ${orgName}`}
          >
            <SelectValue>{(value) => LABEL[value as OrgAccess]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="none">{LABEL.none}</SelectItem>
              <SelectItem value="viewer">{LABEL.viewer}</SelectItem>
              <SelectItem value="editor">{LABEL.editor}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}
