import React from "react";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useFieldArray, useForm } from "react-hook-form";

interface Grantee {
  type: "did" | "clique";
  value: string;
}

interface putData {
  collection: string;
  record: string;
  repo: string;
  rkey: string;
  grantees: Grantee[];
}

interface getData {
  collection: string;
  repo: string;
  rkey: string;
}

export const Route = createFileRoute("/_requireAuth/pear-test/")({
  component() {
    const { authManager } = Route.useRouteContext();
    const putForm = useForm<putData>({
      defaultValues: { grantees: [] },
    });
    const { fields, append, remove } = useFieldArray({
      control: putForm.control,
      name: "grantees",
    });
    const getForm = useForm<getData>({});

    const {
      mutate: put,
      isPending: putIsPending,
      status,
      error: putError,
    } = useMutation({
      async mutationFn(data: putData) {
        let recordObj;
        try {
          recordObj = JSON.parse(data.record);
        } catch {
          alert("Record must be valid JSON");
          return;
        }
        const grantees = data.grantees
          .filter((g) => g.value.trim() !== "")
          .map((g) =>
            g.type === "did"
              ? { $type: "network.habitat.grantee#didGrantee", did: g.value }
              : { $type: "network.habitat.grantee#cliqueRef", uri: g.value },
          );
        const response = await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            collection: data.collection,
            record: recordObj,
            repo: data.repo,
            rkey: data.rkey,
            ...(grantees.length > 0 ? { grantees } : {}),
          }),
        );
        if (!response.ok) {
          const body = await response.text();
          throw new Error(
            body || `Request failed with status ${response.status}`,
          );
        }
      },
    });

    const [fetchedRecord, setFetchedRecord] = React.useState<string>("");
    const {
      mutate: get,
      isPending: getIsPending,
      status: getStatus,
      error: getError,
    } = useMutation({
      async mutationFn(data: getData) {
        const params = new URLSearchParams();
        params.set("collection", data.collection);
        params.set("repo", data.repo);
        params.set("rkey", data.rkey);
        const response = await authManager?.fetch(
          `/xrpc/network.habitat.getRecord?${params.toString()}`,
        );
        if (!response?.ok) {
          const body = await response?.text();
          throw new Error(`[${response?.status}] ${body || "Request failed"}`);
        }
        const json = await response.json();
        setFetchedRecord(JSON.stringify(json.value));
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
                defaultValue={authManager.getAuthInfo()?.did || ""}
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
            <fieldset>
              <legend>Grantees</legend>
              {fields.map((field, index) => (
                <div
                  key={field.id}
                  style={{
                    display: "flex",
                    gap: "0.5rem",
                    alignItems: "center",
                    marginBottom: "0.5rem",
                  }}
                >
                  <select {...putForm.register(`grantees.${index}.type`)}>
                    <option value="did">DID</option>
                    <option value="clique">Clique URI</option>
                  </select>
                  <input
                    type="text"
                    placeholder={
                      putForm.watch(`grantees.${index}.type`) === "did"
                        ? "did:plc:..."
                        : "habitat://..."
                    }
                    {...putForm.register(`grantees.${index}.value`)}
                  />
                  <button
                    type="button"
                    onClick={() => remove(index)}
                    style={{ width: "auto" }}
                  >
                    &times;
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={() => append({ type: "did", value: "" })}
                style={{ width: "auto" }}
              >
                + Add Grantee
              </button>
            </fieldset>
            <button type="submit" aria-busy={putIsPending}>
              Put Record
            </button>
            {status === "success" && "success"}
            {status === "error" && (
              <pre style={{ color: "red" }}>{putError?.message}</pre>
            )}
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
                defaultValue={authManager.getAuthInfo()?.did || ""}
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
            {getStatus === "success" && (
              <pre>Fetched record: {fetchedRecord}</pre>
            )}
            {getStatus === "error" && (
              <pre style={{ color: "red" }}>{getError?.message}</pre>
            )}
          </form>
        </article>
      </div>
    );
  },
});
