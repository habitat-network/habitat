import { useMutation } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
  Spinner,
} from "internal/components/ui";

const ConnectGoogleDialog = () => {
  const { authManager } = useRouteContext({ from: "/_requireAuth" });
  const {
    data,
    isPending,
    mutate: startConnect,
  } = useMutation<{ authUrl: string }>({
    mutationFn: async () => {
      const headers = new Headers();
      headers.set(
        "atproto-proxy",
        `did:web:calendar-server.dwelf-mirzam.ts.net#calendar`,
      );
      const response = await authManager.fetch(
        "/xrpc/network.habitat.calendar.connectGoogle",
        "POST",
        undefined,
        headers,
      );
      return response.json();
    },
  });
  return (
    <Dialog>
      <DialogTrigger
        render={<Button>Connect Google</Button>}
        onClick={() => startConnect()}
      />
      <DialogContent>
        <DialogTitle>Connect Google</DialogTitle>
        <DialogDescription>
          Connect your Google account to sync events with your calendar.
        </DialogDescription>
        <Button
          disabled={isPending}
          render={<a href={data?.authUrl} target="_blank" />}
        >
          {isPending && <Spinner />}
          Connect
        </Button>
      </DialogContent>
    </Dialog>
  );
};

export default ConnectGoogleDialog;
