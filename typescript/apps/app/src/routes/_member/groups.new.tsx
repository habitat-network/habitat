import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  Textarea,
  toast,
} from "internal/components/ui";
import { useForm } from "react-hook-form";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { myGroupsQuery } from "@/lib/queries/groups";
import { createGroup } from "@/server/fns/groups";

// Create a group: its name and handle, and its profile. The
// member becomes its admin.

export const Route = createFileRoute("/_member/groups/new")({
  component: CreateGroupPage,
});

type CreateGroupValues = {
  name: string;
  handle: string;
  description: string;
};

const HANDLE_PATTERN = /^[a-z0-9]{1,50}$/;

function CreateGroupPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const {
    register,
    handleSubmit,
    setValue,
    getFieldState,
    formState: { errors, isValid },
  } = useForm<CreateGroupValues>({
    mode: "onChange",
    defaultValues: {
      name: "",
      handle: "",
      description: "",
    },
  });
  const create = useMutation({
    mutationFn: (v: CreateGroupValues) => createGroup({ data: v }),
    onSuccess: async ({ org }) => {
      await queryClient.invalidateQueries({ queryKey: myGroupsQuery.queryKey });
      await navigate({ to: "/g/$org/files", params: { org } });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't create the group",
        description: e.message,
      }),
  });

  return (
    <>
      <PageHeader title="Create a group" />
      <PageBody className="max-w-2xl">
        <form onSubmit={handleSubmit((v) => create.mutate(v))}>
          <Card>
            <CardHeader>
              <CardTitle>New group</CardTitle>
              <CardDescription>
                A group owns its own data: its docs, files, roles and members.
                You'll be its admin.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-5">
              <Field>
                <FieldLabel htmlFor="new-name">Name</FieldLabel>
                <Input
                  id="new-name"
                  autoFocus
                  {...register("name", {
                    validate: (v) => v.trim() !== "",
                    onChange: (e) => {
                      // The handle follows the name until it's edited by hand.
                      if (!getFieldState("handle").isDirty)
                        setValue(
                          "handle",
                          e.target.value
                            .toLowerCase()
                            .replace(/[^a-z0-9]/g, "")
                            .slice(0, 50),
                          { shouldValidate: true },
                        );
                    },
                  })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="new-handle">Handle</FieldLabel>
                <Input
                  id="new-handle"
                  aria-invalid={!!errors.handle}
                  {...register("handle", {
                    validate: (v) => HANDLE_PATTERN.test(v),
                    setValueAs: (v: string) => v.toLowerCase(),
                  })}
                />
                <FieldDescription>
                  Lowercase letters and digits. It becomes the group's handle on
                  this Habitat instance, which is how people find it.
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="new-description">Description</FieldLabel>
                <Textarea id="new-description" {...register("description")} />
              </Field>
              <Field>
                <FieldLabel>Starter roles</FieldLabel>
                <div className="flex gap-2">
                  <Badge>Admin</Badge>
                  <Badge variant="secondary">Member</Badge>
                </div>
                <FieldDescription>
                  Every group starts with these. Add more under Roles & access.
                </FieldDescription>
              </Field>
            </CardContent>
            <CardFooter className="justify-end">
              <Button type="submit" disabled={!isValid || create.isPending}>
                Create group
              </Button>
            </CardFooter>
          </Card>
        </form>
      </PageBody>
    </>
  );
}
