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
import { procedure, query, SingleHandleCombobox } from "internal";
import { NetworkHabitatOrgCreate } from "api";
import { useEffect, useState } from "react";
import { slugifyHandle } from "@/lib/slugifyHandle";

export const Route = createFileRoute("/community/create")({
  component: CreateCommunityPage,
});

interface FormValues {
  name: string;
  handle_subdomain: string;
  contact_email: string;
  login_method: "atproto" | "google";
  login_id: string;
  use_custom_instance: boolean;
  custom_domain: string;
}

function CreateCommunityPage() {
  const navigate = useNavigate();
  const [step, setStep] = useState<1 | 2>(1);
  const [subdomainTouched, setSubdomainTouched] = useState(false);

  const [customInstanceName, setCustomInstanceName] = useState<string | null>(
    null,
  );
  const [customInstanceError, setCustomInstanceError] = useState<
    string | null
  >(null);

  const {
    register,
    handleSubmit,
    setError,
    setValue,
    watch,
    trigger,
    formState: { isSubmitting, errors },
    control,
  } = useForm<FormValues>({
    defaultValues: {
      name: "",
      handle_subdomain: "",
      contact_email: "",
      login_method: "atproto",
      login_id: "",
      use_custom_instance: false,
      custom_domain: "",
    },
  });

  const loginMethod = watch("login_method");
  const useCustomInstance = watch("use_custom_instance");
  const customDomain = watch("custom_domain");
  const contactEmail = watch("contact_email");

  useEffect(() => {
    if (!useCustomInstance || !customDomain) {
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
      .then((result: { name: string }) => {
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
  }, [useCustomInstance, customDomain]);

  const targetDomain = useCustomInstance ? customDomain : __HABITAT_DOMAIN__;

  const handleNameChange = (value: string) => {
    setValue("name", value);
    if (!subdomainTouched) {
      setValue("handle_subdomain", slugifyHandle(value));
    }
  };

  const handleSubdomainChange = (value: string) => {
    setSubdomainTouched(true);
    setValue("handle_subdomain", value);
  };

  const onContinue = async () => {
    const valid = await trigger(["name", "contact_email"]);
    if (valid) setStep(2);
  };

  const onSubmit = async (values: FormValues) => {
    try {
      const loginId =
        values.login_method === "google" ? contactEmail : values.login_id;
      const body: NetworkHabitatOrgCreate.InputSchema = {
        admin_handle: "admin",
        name: values.name,
        handle_subdomain: values.handle_subdomain,
        login_method: values.login_method,
        login_id: loginId || undefined,
      };
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
      <h1 className="text-2xl font-semibold">Create Your Community</h1>
      <form onSubmit={handleSubmit(onSubmit)}>
        <fieldset disabled={isSubmitting} className="flex flex-col gap-4">
          {step === 1 ? (
            <>
              <Field>
                <FieldLabel>Community Name</FieldLabel>
                <Input
                  placeholder="My Community"
                  {...register("name", { required: true })}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    handleNameChange(e.target.value)
                  }
                />
                <FieldError errors={[errors.name]} />
              </Field>
              <Field>
                <FieldLabel>Handle Subdomain</FieldLabel>
                <Input
                  placeholder="mycommunity"
                  {...register("handle_subdomain", { required: true })}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    handleSubdomainChange(e.target.value)
                  }
                />
                <FieldError errors={[errors.handle_subdomain]} />
              </Field>
              <Field>
                <FieldLabel>Your Email Address</FieldLabel>
                <Input
                  type="email"
                  placeholder="you@example.com"
                  {...register("contact_email", { required: true })}
                />
                <FieldError errors={[errors.contact_email]} />
              </Field>
              <Button type="button" onClick={onContinue}>
                Continue
              </Button>
            </>
          ) : (
            <>
              <Field>
                <FieldLabel>How do you want members to sign in?</FieldLabel>
                <Controller
                  control={control}
                  name="login_method"
                  render={({ field: { onChange, value, ...field } }) => (
                    <ToggleGroup
                      variant="outline"
                      {...field}
                      value={[value]}
                      onValueChange={(newValue) => {
                        onChange(newValue[0]);
                        setValue("login_id", "");
                      }}
                    >
                      <ToggleGroupItem value="atproto">
                        AT Protocol
                      </ToggleGroupItem>
                      <ToggleGroupItem value="google">Google</ToggleGroupItem>
                    </ToggleGroup>
                  )}
                />
              </Field>
              {loginMethod === "atproto" ? (
                <Field>
                  <FieldLabel>Your AT Protocol Handle</FieldLabel>
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
                  <FieldLabel>Your Google Account</FieldLabel>
                  <Input value={contactEmail} disabled />
                </Field>
              )}
              <Field>
                <FieldLabel>How do you want your community hosted?</FieldLabel>
                <ToggleGroup
                  variant="outline"
                  value={[useCustomInstance ? "custom" : "managed"]}
                  onValueChange={(v) =>
                    setValue("use_custom_instance", v[0] === "custom")
                  }
                >
                  <ToggleGroupItem value="managed">
                    Habitat Managed (Recommended)
                  </ToggleGroupItem>
                  <ToggleGroupItem value="custom">Custom</ToggleGroupItem>
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
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setStep(1)}
                >
                  Back
                </Button>
                <Button type="submit">
                  {isSubmitting ? "Creating..." : "Create Community"}
                </Button>
              </div>
            </>
          )}
          <FieldError errors={[errors.root]} />
        </fieldset>
      </form>
    </div>
  );
}
