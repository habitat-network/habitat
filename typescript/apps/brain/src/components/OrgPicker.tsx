import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "internal/components/ui";
import type { Org } from "@/server/habitat";
import { useOrgStore } from "@/stores/org";

// OrgPicker is shown until the member picks the org Brain works in.
export function OrgPicker({ orgs }: { orgs: Org[] }) {
  const selectOrg = useOrgStore((s) => s.selectOrg);
  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Choose an organization</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          {orgs.length === 0 && (
            <p className="text-muted-foreground">
              You aren't a member of any organization yet.
            </p>
          )}
          {orgs.map((org) => (
            <Button
              key={org.did}
              variant="outline"
              onClick={() => selectOrg(org.did)}
            >
              {org.name}
            </Button>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
