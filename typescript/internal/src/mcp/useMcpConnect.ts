import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "../components/ui/toast";

// The type of the message pear's MCP OAuth callback page posts to the window
// that opened it (pearserver.mcpOAuthCallbackMessageType in Go).
const OAUTH_CALLBACK_MESSAGE_TYPE = "habitat-mcp-oauth";

// useMcpConnect connects the member to an oauth MCP server: it opens a popup
// for the server's authorization URL and, once the popup closes (whether the
// member finished or gave up), calls onSettled so the caller can refetch the
// servers' connected status. How the authorization URL is obtained differs by
// app (frontend calls pear straight from the browser, brain through a server
// function), so it's passed in as start.
export function useMcpConnect({
  start,
  onSettled,
}: {
  start: (id: string) => Promise<string | undefined>;
  onSettled: () => void;
}) {
  // The popup the member authorizes in, while it's open. Its lifecycle
  // belongs to the browser rather than to a mutation, so it's tracked
  // separately.
  const [popup, setPopup] = useState<Window | null>(null);
  const pollRef = useRef<number | undefined>(undefined);
  const settledRef = useRef(onSettled);
  settledRef.current = onSettled;

  useEffect(() => {
    if (!popup) return;
    const done = () => {
      window.clearInterval(pollRef.current);
      setPopup(null);
      settledRef.current();
    };
    const onMessage = (event: MessageEvent) => {
      if (
        event.source === popup &&
        event.data?.type === OAUTH_CALLBACK_MESSAGE_TYPE
      ) {
        done();
      }
    };
    window.addEventListener("message", onMessage);
    pollRef.current = window.setInterval(() => {
      if (popup.closed) done();
    }, 500);
    return () => {
      window.removeEventListener("message", onMessage);
      window.clearInterval(pollRef.current);
    };
  }, [popup]);

  const {
    mutate,
    isPending: starting,
    error,
  } = useMutation({ mutationFn: start });

  const connect = (id: string) => {
    // Opened right away, in the click's handler, so pop-up blockers allow
    // it; it's pointed at the authorization URL once pear returns one.
    const opened = window.open(
      "",
      "habitat-mcp-oauth",
      "popup,width=600,height=720",
    );
    if (!opened) {
      toast.add({
        type: "error",
        title: "Allow pop-ups for this site to connect",
      });
      return;
    }
    mutate(id, {
      onSuccess(authorizationUrl) {
        if (!authorizationUrl) {
          opened.close();
          return;
        }
        opened.location.href = authorizationUrl;
        setPopup(opened);
      },
      onError() {
        opened.close();
      },
    });
  };

  return { connect, connecting: starting || popup !== null, error };
}
