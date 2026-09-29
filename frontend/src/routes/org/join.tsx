import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { useForm, Controller } from "react-hook-form";
import { useState } from "react";
import { anonymousAgentFor, SingleHandleCombobox } from "internal";
import { useQuery } from "@tanstack/react-query";
import { network } from "api";
import { xrpc } from "@atproto/lex";

export const Route = createFileRoute("/org/join")({
  validateSearch: z.object({
    token: z.string().default(""),
    orgId: z.string().default(""),
  }),
  component: JoinPage,
});

async function fetchOrgMetadata(
  orgId: string,
  token: string,
): Promise<network.habitat.org.getMetadata.$OutputBody> {
  const url = `https://${import.meta.env.VITE_HABITAT_DOMAIN}/xrpc/network.habitat.org.getMetadata?orgId=${encodeURIComponent(orgId)}`;
  const res = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const data = await res.json().catch(() => undefined);
  if (!res.ok) {
    const message = (data as { message?: string } | undefined)?.message;
    throw new Error(message ?? `Request failed: ${res.status}`);
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
  } = useQuery<network.habitat.org.getMetadata.$OutputBody>({
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
      const res = await xrpc(
        anonymousAgentFor(import.meta.env.VITE_HABITAT_DOMAIN),
        network.habitat.org.mintMemberIdentity.main,
        {
          body: {
            token,
            orgId,
            handle: values.handle,
            password: loginMethod === "password" ? values.password : undefined,
            loginID: loginMethod !== "password" ? values.loginID : undefined,
          },
        },
      );
      setResult({ handle: res.body.handle, did: res.body.did });
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
