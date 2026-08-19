import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import { createFileRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/PageHeader";
import { HelpDialog } from "@/components/HelpDialog";
import { useYDoc } from "@/hooks/useYDoc";

export const Route = createFileRoute("/_requireAuth/$uri")({
  component() {
    const { uri } = Route.useParams();
    const ydoc = useYDoc(uri);

    const editor = useEditor(
      {
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
      },
      [ydoc],
    );

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <HelpDialog />
        </PageHeader>
      </div>
    );
  },
  pendingComponent: () => <article>Loading...</article>,
});
