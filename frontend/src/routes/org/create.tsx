import {
  Button,
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  Input,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { Controller, useForm } from "react-hook-form";
import { procedure, query, SingleHandleCombobox } from "internal";
import { NetworkHabitatOrgCreate } from "api";
import { SetStateAction, useEffect, useState } from "react";

export const Route = createFileRoute("/org/create")({
  validateSearch: z.object({
    token: z.string().optional(),
  }),
  component: CreateOrgPage,
});

interface FormValues {
  name: string;
  admin_handle: string;
  admin_password: string;
  login_method: "password" | "atproto" | "google";
  login_id: string;
  contact_email: string;
  handle_subdomain: string;
  use_custom_instance: boolean;
  custom_domain: string;
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

  const [customInstanceName, setCustomInstanceName] = useState<string | null>(
    null,
  );
  const [customInstanceError, setCustomInstanceError] = useState<string | null>(
    null,
  );

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
      contact_email: "",
      use_custom_instance: false,
      custom_domain: "",
    },
  });

  const loginMethod = watch("login_method");
  const useCustomInstance = watch("use_custom_instance");
  const customDomain = watch("custom_domain");
  const googleLoginId = watch("login_id");

  const [contactEmailTouched, setContactEmailTouched] = useState(false);

  useEffect(() => {
    if (loginMethod === "google" && !contactEmailTouched) {
      setValue("contact_email", googleLoginId);
    }
  }, [loginMethod, googleLoginId, contactEmailTouched, setValue]);

  useEffect(() => {
    if (invite || !useCustomInstance || !customDomain) {
      setCustomInstanceName(null);
      setCustomInstanceError(null);
      return;
    }
    let cancelled = false;
    query(
      "network.habitat.instance.describeInstance",
      {},
      { unauthenticated: true, domain: customDomain },
    )
      .then((result: { name: SetStateAction<string | null> }) => {
        if (!cancelled) {
          setCustomInstanceName(result.name);
          setCustomInstanceError(null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCustomInstanceName(null);
          setCustomInstanceError("Could not reach that instance.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [invite, useCustomInstance, customDomain]);

  const targetDomain = invite
    ? invite.domain
    : useCustomInstance
      ? customDomain
      : __HABITAT_DOMAIN__;

  const onSubmit = async (values: FormValues) => {
    try {
      let body: NetworkHabitatOrgCreate.InputSchema = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
        handle_subdomain: values.handle_subdomain,
        contact_email: values.contact_email,
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
        search: { handle: admin_handle },
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
            <Field>
              <FieldLabel>Hosting</FieldLabel>
              <ToggleGroup
                variant="outline"
                value={[useCustomInstance ? "custom" : "managed"]}
                onValueChange={(v) =>
                  setValue("use_custom_instance", v[0] === "custom")
                }
              >
                <ToggleGroupItem value="managed">
                  Managed hosting by Habitat
                </ToggleGroupItem>
                <ToggleGroupItem value="custom">
                  Custom instance
                </ToggleGroupItem>
              </ToggleGroup>
              {useCustomInstance ? (
                <>
                  <Input
                    placeholder="myinstance.example.com"
                    {...register("custom_domain")}
                  />
                  {customInstanceName ? (
                    <Input value={customInstanceName} disabled />
                  ) : null}
                  {customInstanceError ? (
                    <p className="text-sm text-destructive">
                      {customInstanceError}
                    </p>
                  ) : null}
                </>
              ) : null}
            </Field>
          )}
          <Field>
            <FieldLabel>Organization Name</FieldLabel>
            <Input placeholder="My Organization" {...register("name")} />
            <FieldError errors={[errors.name]} />
          </Field>
          <Field>
            <FieldLabel>Contact Email</FieldLabel>
            <FieldDescription>
              For contact purposes about your account.
            </FieldDescription>
            <Input
              type="email"
              placeholder="you@example.com"
              {...register("contact_email", {
                required: true,
                onChange: () => setContactEmailTouched(true),
                pattern: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
              })}
            />
            <FieldError errors={[errors.contact_email]} />
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
