import { HabitatDoc } from "@/habitatDoc";
import { queryOptions } from "@tanstack/react-query";
import { AuthManager, getPrivateRecord, listPrivateRecords } from "internal";

export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    queryFn: () =>
      listPrivateRecords<HabitatDoc>(authManager, "network.habitat.docs"),
  });

export const docQueryOptions = (uri: string, authManager: AuthManager) => {
  const [, , docDID, , rkey] = uri.split("/");
  return queryOptions({
    queryKey: ["doc", uri],
    queryFn: () =>
      getPrivateRecord<HabitatDoc>(
        authManager,
        "network.habitat.docs",
        rkey,
        docDID,
        true,
      ),
  });
};
