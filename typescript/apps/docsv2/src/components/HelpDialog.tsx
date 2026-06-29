import {
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogTrigger,
  Kbd,
} from "internal/components/ui";

export function HelpDialog() {
  return (
    <Dialog>
      <DialogTrigger
        render={
          <Button size="icon" variant="ghost">
            ?
          </Button>
        }
      />
      <DialogContent>
        <DialogTitle>Help</DialogTitle>
        <div className="space-y-4 text-sm">
          <section>
            <h3 className="font-semibold mb-1">Markdown shortcuts</h3>
            <p className="text-muted-foreground mb-2">
              Type these at the beginning of a new line to quickly format
              content.
            </p>
            <ul className="space-y-1 list-none pl-0">
              <li>
                <Kbd>#</Kbd> Heading 1
              </li>
              <li>
                <Kbd>##</Kbd> Heading 2
              </li>
              <li>
                <Kbd>###</Kbd> Heading 3
              </li>
              <li>
                <Kbd>-</Kbd> or <Kbd>*</Kbd> Bullet list
              </li>
              <li>
                <Kbd>1.</Kbd> Numbered list
              </li>
              <li>
                <Kbd>&gt;</Kbd> Blockquote
              </li>
              <li>
                <Kbd>```</Kbd> Code block
              </li>
              <li>
                <Kbd>---</Kbd> Horizontal rule
              </li>
            </ul>
          </section>
          <section>
            <h3 className="font-semibold mb-1">Keyboard shortcuts</h3>
            <ul className="space-y-1 list-none pl-0">
              <li>
                <Kbd>⌘ B</Kbd> Bold
              </li>
              <li>
                <Kbd>⌘ I</Kbd> Italic
              </li>
              <li>
                <Kbd>⌘ ⇧ X</Kbd> Strikethrough
              </li>
              <li>
                <Kbd>⌘ E</Kbd> Inline code
              </li>
            </ul>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}
