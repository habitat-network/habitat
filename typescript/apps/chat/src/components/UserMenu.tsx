import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { UserAvatar } from "internal";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  toast,
} from "internal/components/ui";
import { LogOut } from "lucide-react";
import { signOut, type Member } from "@/server/functions";

export function UserMenu({ member }: { member: Member }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { mutate: logOut } = useMutation({
    mutationFn: () => signOut(),
    onSuccess: async () => {
      await router.navigate({ to: "/login" });
      queryClient.clear();
      await router.invalidate();
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't sign out",
        description: error.message,
      }),
  });
  const actor = { did: member.did, handle: member.handle ?? undefined };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="rounded-full outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
        <UserAvatar actor={actor} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="truncate">
            {member.handle ? `@${member.handle}` : member.did}
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => logOut()}>
          <LogOut />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
