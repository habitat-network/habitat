import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm, Controller } from "react-hook-form";
import { useState } from "react";
import { procedure, SingleHandleCombobox, XRPCError } from "internal";
import { useQuery } from "@tanstack/react-query";
import { NetworkHabitatOrgGetMetadata } from "api";

export const Route = createFileRoute("/org/join")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: String(search.token ?? ""),
    orgId: String(search.orgId ?? ""),
  }),
  component: JoinPage,
});

async function fetchOrgMetadata(
  orgId: string,
  token: string,
): Promise<NetworkHabitatOrgGetMetadata.OutputSchema> {
  const url = `https://${__HABITAT_DOMAIN__}/xrpc/network.habitat.org.getMetadata?orgId=${encodeURIComponent(orgId)}`;
  const res = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const data = await res.json().catch(() => undefined);
  if (!res.ok) {
    throw new XRPCError(res.status, data);
  }
  return data;
}

type FormValues = {
  handle: string;
  password: string;
  loginID: string;
};

function JoinPage() {
  const { token, orgId } = Route.useSearch();
  const [result, setResult] = useState<{ handle: string; did: string } | null>(
    null,
  );

  const {
    data: metadata,
    isLoading: metadataLoading,
    error: metadataError,
  } = useQuery<NetworkHabitatOrgGetMetadata.OutputSchema>({
    queryKey: ["orgMetadata", orgId],
    queryFn: () => fetchOrgMetadata(orgId, token),
    enabled: !!orgId && !!token,
    retry: false,
  });

  const loginMethod = metadata?.loginMethod ?? "password";
  const handleSubdomain = metadata?.handleSubdomain ?? "";
  const orgName = metadata?.name ?? handleSubdomain;

  const {
    register,
    handleSubmit,
    setError,
    control,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>();

  const onSubmit = async (values: FormValues) => {
    try {
      const res = await procedure(
        "network.habitat.org.mintMemberIdentity",
        {
          token,
          orgId,
          handle: values.handle,
          password: loginMethod === "password" ? values.password : undefined,
          loginID: loginMethod !== "password" ? values.loginID : undefined,
        },
        { unauthenticated: true, domain: __HABITAT_DOMAIN__ },
      );
      setResult({ handle: res.handle, did: res.did });
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  if (result) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <h1 className="text-2xl font-semibold">Welcome!</h1>
        <p className="text-muted-foreground">Your account has been created.</p>
        <div className="flex flex-col gap-1 text-sm font-mono">
          <span>{result.handle}</span>
          <span className="text-muted-foreground">{result.did}</span>
        </div>
      </div>
    );
  }

  if (metadataLoading) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <p className="text-muted-foreground">Loading...</p>
      </div>
    );
  }

  if (metadataError || !metadata) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <p className="text-muted-foreground">Invalid invite token.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">Join {orgName}</h1>
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
        <Field>
          <FieldLabel>
            Handle
            {loginMethod === "password" ? (
              <span className="text-gray-400 text-sm ml-1 font-normal">
                This will look like your-handle.{handleSubdomain}
              </span>
            ) : null}
          </FieldLabel>
          <Input
            placeholder="handle"
            disabled={isSubmitting}
            {...register("handle", { required: true })}
          />
          <FieldError errors={[errors.handle]} />
        </Field>
        {loginMethod === "password" ? (
          <Field>
            <FieldLabel>Password</FieldLabel>
            <Input
              type="password"
              placeholder="password"
              disabled={isSubmitting}
              {...register("password", { required: true })}
            />
            <FieldError errors={[errors.password]} />
          </Field>
        ) : loginMethod === "atproto" ? (
          <Field>
            <FieldLabel>AT Protocol Handle</FieldLabel>
            <Controller
              control={control}
              name="loginID"
              rules={{ required: true }}
              render={({ field: { onChange, value } }) => (
                <SingleHandleCombobox
                  value={value ?? ""}
                  onValueChange={onChange}
                />
              )}
            />
            <FieldError errors={[errors.loginID]} />
          </Field>
        ) : (
          <Field>
            <FieldLabel>Google Email</FieldLabel>
            <Controller
              control={control}
              name="loginID"
              rules={{ required: true }}
              render={({ field: { onChange, value } }) => (
                <Input
                  placeholder="user@gmail.com"
                  value={value ?? ""}
                  onChange={(e) => onChange(e.target.value)}
                />
              )}
            />
            <FieldError errors={[errors.loginID]} />
          </Field>
        )}
        <FieldError errors={[errors.root]} />
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Joining..." : "Join"}
        </Button>
      </form>
    </div>
  );
}
