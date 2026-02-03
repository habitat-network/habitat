import React from "react";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";

interface putData {
  collection: string;
  record: string;
  repo: string;
  rkey: string;
}

interface getData {
  collection: string;
  repo: string;
  rkey: string;
}

export const Route = createFileRoute("/_requireAuth/pear-test/")({
  component() {
    const { authManager } = Route.useRouteContext();
    const putForm = useForm<putData>({});
    const getForm = useForm<getData>({});

    const {
      mutate: put,
      isPending: putIsPending,
      status,
    } = useMutation({
      async mutationFn(data: putData) {
        let recordObj;
        try {
          recordObj = JSON.parse(data.record);
        } catch {
          alert("Record must be valid JSON");
          return;
        }
        const response = await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            collection: data.collection,
            record: recordObj,
            repo: data.repo,
            rkey: data.rkey,
          }),
        );
        console.log(response);
      },
    });

    const [fetchedRecord, setFetchedRecord] = React.useState<string>("");
    const { mutate: get, isPending: getIsPending } = useMutation({
      async mutationFn(data: getData) {
        const params = new URLSearchParams();
        params.set("collection", data.collection);
        params.set("repo", data.repo);
        params.set("rkey", data.rkey);
        const response = await authManager?.fetch(
          `/xrpc/network.habitat.getRecord?${params.toString()}`,
        );
        const json = await response?.json();
        const val = JSON.stringify(json.value);
        setFetchedRecord(val);
      },
    });

    return (
      <div
        style={{
          display: "flex",
          flexDirection: "row",
          alignItems: "flex-start",
          justifyContent: "space-between",
        }}
      >
        <article>
          <h1>putRecord</h1>
          <form onSubmit={putForm.handleSubmit((data) => put(data))}>
            <label>
              collection:
              <input
                type="text"
                defaultValue="network.habitat.test"
                {...putForm.register("collection")}
              />
            </label>
            <label>
              record (JSON):
              <input
                type="text"
                defaultValue='{"foo":"bar"}'
                {...putForm.register("record")}
              />
            </label>
            <label>
              repo:
              <input
                type="text"
                defaultValue={authManager.handle || ""}
                {...putForm.register("repo")}
              />
            </label>
            <label>
              rkey:
              <input
                type="text"
                defaultValue="a-primary-key"
                {...putForm.register("rkey")}
              />
            </label>
            <button type="submit" aria-busy={putIsPending}>
              Put Record
            </button>
            {status !== "idle" && status}
          </form>
        </article>

        <article>
          <h1>getRecord</h1>
          <form onSubmit={getForm.handleSubmit((data) => get(data))}>
            <label>
              collection:
              <input
                type="text"
                defaultValue="network.habitat.test"
                {...getForm.register("collection")}
              />
            </label>
            <label>
              repo:
              <input
                type="text"
                defaultValue={authManager.handle || ""}
                {...getForm.register("repo")}
              />
            </label>
            <label>
              rkey:
              <input
                type="text"
                defaultValue="a-primary-key"
                {...getForm.register("rkey")}
              />
            </label>
            <button type="submit" aria-busy={getIsPending}>
              Get Record
            </button>
            Fetched record: {fetchedRecord}
          </form>
        </article>
      </div>
    );
  },
});
