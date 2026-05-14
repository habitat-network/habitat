import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { procedure } from "internal";

export const Route = createFileRoute("/org/create")({
  component: CreateOrgPage,
});

interface FormValues {
  name: string;
  admin_handle: string;
  admin_password: string;
}

function CreateOrgPage() {
  const navigate = useNavigate();
  const {
    register,
    handleSubmit,
    setError,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>({
    defaultValues: {
      admin_handle: "admin",
      admin_password: "12345",
      name: "My Organization",
    },
  });

  const onSubmit = async (values: FormValues) => {
    try {
      await procedure(
        "network.habitat.org.create",
        {
          admin_handle: values.admin_handle,
          admin_password: values.admin_password,
          name: values.name || undefined,
        },
        { unauthenticated: true, domain: __HABITAT_DOMAIN__ },
      );
      await navigate({
        to: "/oauth-login",
        search: { handle: values.admin_handle },
      });
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  return (
    <div className="flex flex-col gap-4 mt-16">
      <h1 className="text-2xl font-semibold">Create Organization</h1>
      <form onSubmit={handleSubmit(onSubmit)}>
        <fieldset disabled={isSubmitting} className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Organization Name</FieldLabel>
            <Input placeholder="My Organization" {...register("name")} />
            <FieldError errors={[errors.name]} />
          </Field>
          <Field>
            <FieldLabel>Admin Handle</FieldLabel>
            <Input
              placeholder="admin"
              {...register("admin_handle", { required: true })}
            />
            <FieldError errors={[errors.admin_handle]} />
          </Field>
          <Field>
            <FieldLabel>Admin Password</FieldLabel>
            <Input
              type="password"
              placeholder="password"
              {...register("admin_password", { required: true })}
            />
            <FieldError errors={[errors.admin_password]} />
          </Field>
          <FieldError errors={[errors.root]} />
          <Button type="submit">
            {isSubmitting ? "Creating..." : "Create Organization"}
          </Button>
        </fieldset>
      </form>
    </div>
  );
}
