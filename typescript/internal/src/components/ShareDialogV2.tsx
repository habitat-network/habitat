import { useState, type ReactElement } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  NetworkHabitatRelationshipTuple,
  NetworkHabitatRelationshipDefs,
  NetworkHabitatGroupProfile,
} from "api";
import { UsersIcon } from "lucide-react";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "./ui/dialog";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Spinner } from "./ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import { UserAvatar } from "./UserAvatar";
import { GroupCombobox, type GroupView } from "./GroupCombobox";
import { AuthManager } from "../authManager";
import { procedure, query } from "../habitatClient";
import { resolveDidToHandle, resolveHandleToDid } from "../atprotoDirectory";

const TUPLE_COLLECTION = "network.habitat.relationship.tuple";
const GROUP_PROFILE_COLLECTION = "network.habitat.group.profile";
const USER_SUBJECT = "network.habitat.relationship.defs#userSubject";
const SPACE_ROLE_SUBJECT = "network.habitat.relationship.defs#spaceRoleSubject";
// The group-space role that represents membership; granting this userset shares
// with everyone in the group (see network.habitat.group.profile).
const GROUP_MEMBER_ROLE = "writer";

type Relation = "owner" | "manager" | "writer" | "reader";

interface SharedUser {
  did: string;
  handle?: string;
}

interface SharedGroup {
  uri: string;
  name: string;
}

interface ShareState {
  users: SharedUser[];
  groups: SharedGroup[];
}

// The space URI's second path segment is the owning org DID, whose repo holds
// the tuple and group-profile records (at://<orgDid>/<collection>/<rkey>).
function ownerDid(spaceUri: string): string {
  return spaceUri.split("/")[2];
}

// loadShareState reads the space's relationship tuples and resolves them into
// the users and groups that currently have access. User DIDs are resolved to
// handles through the atproto directory; space-role subjects are kept only when
// they resolve to a group (i.e. expose a group profile), which is how we filter
// to network.habitat.group spaces.
async function loadShareState(
  spaceUri: string,
  authManager: AuthManager,
): Promise<ShareState> {
  const { records } = await query(
    "network.habitat.space.listRecords",
    { space: spaceUri, repo: ownerDid(spaceUri), collection: TUPLE_COLLECTION },
    { authManager },
  );

  const userDids = new Set<string>();
  const groupSpaces = new Set<string>();
  for (const record of records) {
    const tuple = record.value as NetworkHabitatRelationshipTuple.Record;
    const subject = tuple.subject;
    if (subject.$type === USER_SUBJECT) {
      userDids.add((subject as NetworkHabitatRelationshipDefs.UserSubject).did);
    } else if (subject.$type === SPACE_ROLE_SUBJECT) {
      groupSpaces.add(
        (subject as NetworkHabitatRelationshipDefs.SpaceRoleSubject).space,
      );
    }
  }

  const users = await Promise.all(
    [...userDids].map(async (did) => ({
      did,
      handle: await resolveDidToHandle(did),
    })),
  );

  const groups = (
    await Promise.all(
      [...groupSpaces].map(async (uri): Promise<SharedGroup | null> => {
        try {
          const record = await query(
            "network.habitat.space.getRecord",
            {
              space: uri,
              repo: ownerDid(uri),
              collection: GROUP_PROFILE_COLLECTION,
              rkey: "self",
            },
            { authManager },
          );
          const profile = record.value as NetworkHabitatGroupProfile.Main;
          return { uri, name: profile.name };
        } catch {
          // Not a group (no profile record) — filter it out.
          return null;
        }
      }),
    )
  ).filter((g): g is SharedGroup => g !== null);

  return { users, groups };
}

interface ShareDialogV2Props {
  spaceUri: string;
  authManager: AuthManager;
  // Role granted to newly added users and groups. Defaults to "reader".
  relation?: Relation;
  // Custom trigger element; defaults to a "Share" button. Must be a single
  // element (base-ui attaches a ref to it).
  trigger?: ReactElement;
}

// ShareDialogV2 is a reusable share modal for a space. It lists the users and
// groups that currently have access and lets the caller grant access to more.
export const ShareDialogV2 = ({
  spaceUri,
  authManager,
  relation = "reader",
  trigger,
}: ShareDialogV2Props) => {
  const queryClient = useQueryClient();
  const queryKey = ["shareState", spaceUri];

  const [userHandle, setUserHandle] = useState("");
  const [selectedGroup, setSelectedGroup] = useState<GroupView | null>(null);

  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: () => loadShareState(spaceUri, authManager),
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey });

  const addUser = useMutation({
    mutationFn: async (handle: string) => {
      const did = await resolveHandleToDid(handle);
      await procedure(
        "network.habitat.relationship.writeTuple",
        {
          subject: { $type: USER_SUBJECT, did },
          relation,
          object: { space: spaceUri },
        },
        { authManager },
      );
    },
    onSuccess: () => {
      setUserHandle("");
      invalidate();
    },
  });

  const addGroup = useMutation({
    mutationFn: async (group: GroupView) => {
      await procedure(
        "network.habitat.relationship.writeTuple",
        {
          subject: {
            $type: SPACE_ROLE_SUBJECT,
            space: group.uri,
            role: GROUP_MEMBER_ROLE,
          },
          relation,
          object: { space: spaceUri },
        },
        { authManager },
      );
    },
    onSuccess: () => {
      setSelectedGroup(null);
      invalidate();
    },
  });

  return (
    <Dialog>
      <DialogTrigger render={trigger ?? <Button>Share</Button>} />
      <DialogContent className="flex flex-col gap-4 sm:max-w-lg">
        <DialogTitle>Share</DialogTitle>

        {/* Add a user by handle */}
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const handle = userHandle.trim();
            if (handle) addUser.mutate(handle);
          }}
        >
          <label className="text-sm font-medium">Add a person</label>
          <div className="flex gap-2">
            <Input
              placeholder="alice.example.com"
              value={userHandle}
              onChange={(e) => setUserHandle(e.target.value)}
            />
            <Button
              type="submit"
              disabled={!userHandle.trim() || addUser.isPending}
            >
              {addUser.isPending && <Spinner />}
              Add
            </Button>
          </div>
          {addUser.isError && (
            <p className="text-destructive text-sm">
              {(addUser.error as Error).message}
            </p>
          )}
        </form>

        {/* Add a group */}
        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium">Add a group</label>
          <div className="flex gap-2">
            <div className="flex-1">
              <GroupCombobox
                authManager={authManager}
                value={selectedGroup}
                onValueChange={setSelectedGroup}
              />
            </div>
            <Button
              disabled={!selectedGroup || addGroup.isPending}
              onClick={() => selectedGroup && addGroup.mutate(selectedGroup)}
            >
              {addGroup.isPending && <Spinner />}
              Add
            </Button>
          </div>
          {addGroup.isError && (
            <p className="text-destructive text-sm">
              {(addGroup.error as Error).message}
            </p>
          )}
        </div>

        {/* Existing access, split into a groups table and a people table. */}
        <div className="flex flex-col gap-4">
          {isLoading && <Spinner />}
          {!isLoading &&
            data?.users.length === 0 &&
            data?.groups.length === 0 && (
              <p className="text-muted-foreground text-sm">
                No one else has access yet.
              </p>
            )}

          {data && data.groups.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Groups with access</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.groups.map((group) => (
                  <TableRow key={group.uri}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <UsersIcon className="size-4 text-muted-foreground" />
                        {group.name}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          {data && data.users.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>People with access</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.users.map((user) => (
                  <TableRow key={user.did}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <UserAvatar
                          actor={{ did: user.did, handle: user.handle }}
                          size="sm"
                        />
                        {user.handle ? `@${user.handle}` : "Unknown User"}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default ShareDialogV2;
