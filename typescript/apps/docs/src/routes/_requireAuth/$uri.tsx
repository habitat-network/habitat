import { Editor, EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { HabitatDoc } from "@/habitatDoc";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const { uri } = params;
    const [, , did, lexicon, rkey] = uri.split("/");
    const response = await context.authManager.fetch(
      `/xrpc/network.habitat.getRecord?repo=${did}&collection=${lexicon}&rkey=${rkey}`,
    );

    const data: {
      uri: string;
      cid: string;
      value: HabitatDoc;
    } = await response?.json();

    return {
      rkey,
      did,
      record: data.value,
    };
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const { record, did, rkey } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const [dirty, setDirty] = useState(false);
    const { mutate: save } = useMutation({
      mutationFn: async ({ editor }: { editor: Editor }) => {
        const heading = editor.$node("heading")?.textContent;
        authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            repo: did,
            collection: "com.habitat.docs",
            rkey,
            record: {
              name: heading ?? "Untitled",
              blob: editor.getHTML(),
            },
          }),
        );
      },
      onSuccess: () => setDirty(false),
    });
    // debounce
    const handleUpdate = useMemo(() => {
      let prevTimeout: number | undefined;
      return ({ editor }: { editor: Editor }) => {
        setDirty(true);
        clearTimeout(prevTimeout);
        prevTimeout = window.setTimeout(() => {
          save({ editor });
        }, 1000);
      };
    }, [save]);
    const editor = useEditor({
      extensions: [StarterKit],
      content: record.blob || "",
      onUpdate: handleUpdate,
    });
    return (
      <>
        <article>
          <EditorContent editor={editor} />
        </article>
        {dirty ? "🔄 Syncing" : "✅ Synced"}
      </>
    );
  },
});
