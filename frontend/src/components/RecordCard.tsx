import { NetworkHabitatRepoGetRecord } from "api";
import { Card, CardContent, CardFooter, CardTitle, UserAvatar } from "internal";

export type RecordCardProps = Omit<
  NetworkHabitatRepoGetRecord.OutputSchema,
  "permissions"
> & {
  // updatedAt: string; // TODO: add this later
  grantees: { did: string; avatar?: string; handle: string }[];
};

export function RecordCard(record: RecordCardProps) {
  return (
    <Card>
      <CardTitle>{record.uri}</CardTitle>
      <CardContent>{JSON.stringify(record.value).slice(0, 100)}</CardContent>
      <CardFooter>
        <div className="flex gap-1">
          {record.grantees.map((g) => {
            return <UserAvatar key={g.did} actor={g} size="sm" />;
          })}
        </div>
      </CardFooter>
    </Card>
  );
}
