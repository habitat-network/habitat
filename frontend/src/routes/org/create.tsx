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
import { HostingSelector, procedure, SingleHandleCombobox } from "internal";
import { NetworkHabitatOrgCreate } from "api";
import { useState } from "react";

export const Route = createFileRoute("/org/create")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: search.token ? String(search.token) : undefined,
  }),
  component: CreateOrgPage,
});

interface FormValues {
  name: string;
  admin_handle: string;
  admin_password: string;
  login_method: "password" | "atproto" | "google";
  login_id: string;
  handle_subdomain: string;
}

interface DecodedInvite {
  domain: string;
  name: string;
}

function decodeInviteToken(token: string): DecodedInvite | null {
  try {
    const payload = token.split(".")[1];
    const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
    const claims = JSON.parse(json);
    if (typeof claims.domain !== "string" || typeof claims.name !== "string") {
      return null;
    }
    return { domain: claims.domain, name: claims.name };
  } catch {
    return null;
  }
}

function PasswordInput(props: React.ComponentProps<typeof Input>) {
  const [isPlain, setIsPlain] = useState(true);

  return (
    <Input
      type={isPlain ? "text" : "password"}
      {...props}
      onFocus={() => setIsPlain(false)}
    />
  );
}

function CreateOrgPage() {
  const navigate = useNavigate();
  const { token } = Route.useSearch();
  const invite = token ? decodeInviteToken(token) : null;

  const [domain, setDomain] = useState(invite?.domain ?? __HABITAT_DOMAIN__);

  const {
    register,
    handleSubmit,
    setError,
    setValue,
    watch,
    formState: { isSubmitting, errors },
    control,
  } = useForm<FormValues>({
    defaultValues: {
      admin_handle: "admin",
      admin_password: "",
      handle_subdomain: "acmecorp",
      name: "My Organization",
      login_method: "password",
      login_id: "",
    },
  });

  const loginMethod = watch("login_method");

  const targetDomain = invite ? invite.domain : domain;

  const onSubmit = async (values: FormValues) => {
    try {
      let body: NetworkHabitatOrgCreate.InputSchema = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
        handle_subdomain: values.handle_subdomain,
        invite_token: token,
      };
      if (values.login_method === "password") {
        body.admin_password = values.admin_password;
      } else {
        body.login_id = values.login_id || undefined;
      }
      const { admin_handle } = await procedure(
        "network.habitat.org.create",
        body,
        { unauthenticated: true, domain: targetDomain },
      );
      await navigate({
        to: "/oauth-login",
        search: { handle: admin_handle, domain: targetDomain },
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
          {invite ? (
            <Field>
              <FieldLabel>Hosted on</FieldLabel>
              <Input value={invite.name} disabled />
            </Field>
          ) : (
            <HostingSelector
              defaultDomain={__HABITAT_DOMAIN__}
              value={domain}
              onChange={setDomain}
            />
          )}
          <Field>
            <FieldLabel>Organization Name</FieldLabel>
            <Input placeholder="My Organization" {...register("name")} />
            <FieldError errors={[errors.name]} />
          </Field>
          <Field>
            <FieldLabel>Handle Subdomain</FieldLabel>
            <Input
              placeholder="acmecorp"
              {...register("handle_subdomain", { required: true })}
            />
            <FieldError errors={[errors.handle_subdomain]} />
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
                return (
                  <ToggleGroup
                    variant="outline"
                    {...field}
                    value={[value]}
                    onValueChange={(newValue) => {
                      onChange(newValue[0]);
                      setValue("login_id", "");
                      setValue("admin_password", "");
                    }}
                  >
                    <ToggleGroupItem value="password">Password</ToggleGroupItem>
                    <ToggleGroupItem value="atproto">
                      AT Protocol
                    </ToggleGroupItem>
                    <ToggleGroupItem value="google">Google</ToggleGroupItem>
                  </ToggleGroup>
                );
              }}
            />
          </Field>
          {loginMethod === "password" ? (
            <Field>
              <FieldLabel>Admin Password</FieldLabel>
              <PasswordInput
                placeholder="password"
                {...register("admin_password", { required: true })}
              />
              <FieldError errors={[errors.admin_password]} />
            </Field>
          ) : loginMethod === "atproto" ? (
            <Field>
              <FieldLabel>AT Protocol Handle</FieldLabel>
              <Controller
                control={control}
                name="login_id"
                rules={{ required: true }}
                render={({ field: { onChange, value } }) => (
                  <SingleHandleCombobox
                    value={value ?? ""}
                    onValueChange={onChange}
                  />
                )}
              />
              <FieldError errors={[errors.login_id]} />
            </Field>
          ) : (
            <Field>
              <FieldLabel>Google Email</FieldLabel>
              <Controller
                control={control}
                name="login_id"
                rules={{ required: true }}
                render={({ field: { onChange, value } }) => (
                  <Input
                    placeholder="user@gmail.com"
                    value={value ?? ""}
                    onChange={(e) => onChange(e.target.value)}
                  />
                )}
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
