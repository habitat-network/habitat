import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// useLiveFiles keeps the org's file list fresh: SyncHub pings this socket
// whenever a sync batch changes the org's files (including uploads from
// other members), and the list refetches. Reconnects with backoff when the
// socket drops.
export function useLiveFiles(orgDid: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    let socket: WebSocket | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    let closed = false;

    const connect = () => {
      const scheme = window.location.protocol === "https:" ? "wss" : "ws";
      socket = new WebSocket(`${scheme}://${window.location.host}/api/live`);
      socket.onopen = () => {
        attempts = 0;
        // Anything that changed while disconnected.
        void queryClient.invalidateQueries({ queryKey: ["files", orgDid] });
      };
      socket.onmessage = () => {
        void queryClient.invalidateQueries({ queryKey: ["files", orgDid] });
      };
      socket.onclose = () => {
        if (closed) return;
        const delay = Math.min(30_000, 1_000 * 2 ** attempts++);
        retry = setTimeout(connect, delay);
      };
    };
    connect();

    return () => {
      closed = true;
      clearTimeout(retry);
      socket?.close();
    };
  }, [orgDid, queryClient]);
}
