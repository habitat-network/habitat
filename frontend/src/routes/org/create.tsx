import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Controller, useForm } from "react-hook-form";
import { procedure } from "internal";
import { useMutation } from "@tanstack/react-query";

export const Route = createFileRoute("/org/create")({
  component: CreateOrgPage,
});

interface FormValues {
  name: string;
  admin_handle: string;
  admin_password: string;
  login_method: "password" | "atproto" | "google";
  login_id: string;
}

function CreateOrgPage() {
  const navigate = useNavigate();
  const {
    register,
    handleSubmit,
    setError,
    watch,
    formState: { isSubmitting, errors },
    control,
  } = useForm<FormValues>({
    defaultValues: {
      admin_handle: "admin",
      admin_password: "12345",
      name: "My Organization",
      login_method: "password",
      login_id: "",
    },
  });

  const loginMethod = watch("login_method");

  const {} = useMutation({
    mutationFn: async (values: FormValues) => {
    try {
      const body: Record<string, string | undefined> = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
      };
      if (values.login_method === "password") {
        body.admin_password = values.admin_password;
      } else {
        body.login_id = values.login_id || undefined;
      }
      await procedure(
        "network.habitat.org.create",
        body,
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
  }
  })

  const onSubmit = async (values: FormValues) => {
    try {
      const body: Record<string, string | undefined> = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
      };
      if (values.login_method === "password") {
        body.admin_password = values.admin_password;
      } else {
        body.login_id = values.login_id || undefined;
      }
      await procedure(
        "network.habitat.org.create",
        body,
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
            <FieldLabel>Login Method</FieldLabel>
            <Controller 
              control={control}
              name="login_method"
              render={({ field: { onChange, value, ...field } }) => {
                return <ToggleGroup
                  variant="outline"
                  {...field}
                  value={[value]}
                  onValueChange={(value) => onChange(value[0])}
              >
                <ToggleGroupItem value="password">Password</ToggleGroupItem>
                <ToggleGroupItem value="atproto">AT Protocol</ToggleGroupItem>
                <ToggleGroupItem value="google">Google</ToggleGroupItem>
              </ToggleGroup>
              }}
            />
          </Field>
          {loginMethod === "password" ? (
            <Field>
              <FieldLabel>Admin Password</FieldLabel>
              <Input
                type="password"
                placeholder="password"
                {...register("admin_password", { required: true })}
              />
              <FieldError errors={[errors.admin_password]} />
            </Field>
          ) : (
            <Field>
              <FieldLabel>
                {loginMethod === "atproto"
                  ? "AT Protocol DID"
                  : "Google Email"}
              </FieldLabel>
              <Input
                placeholder={
                  loginMethod === "atproto"
                    ? "did:plc:..."
                    : "user@gmail.com"
                }
                {...register("login_id", { required: true })}
              />
              <FieldError errors={[errors.login_id]} />
            </Field>
          )}
          <FieldError errors={[errors.root]} />
          <Button type="submit">
            {isSubmitting ? "Creating..." : "Create Organization"}
          </Button>
        </fieldset>
      </form>
    </div>
  );
}
