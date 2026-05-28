import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Combobox,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm, Controller } from "react-hook-form";
import { useState, useEffect, useRef } from "react";
import { procedure, searchActorsTypeahead, UserAvatar } from "internal";
import { useQuery } from "@tanstack/react-query";
import type { Actor } from "internal";

export const Route = createFileRoute("/org/join")({
  validateSearch: (search: Record<string, unknown>) => {
    const token = String(search.token ?? "");
    let orgId = String(search.orgId ?? "");
    if (!orgId && token) {
      const payload = decodeJwtPayload(token);
      orgId = payload?.orgId ?? "";
    }
    return { token, orgId };
  },
  component: JoinPage,
});

function decodeJwtPayload(token: string): Record<string, unknown> | null {
  try {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const decoded = atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"));
    return JSON.parse(decoded);
  } catch {
    return null;
  }
}

type InviteTokenPayload = {
  loginMethod?: string;
  handleSubdomain?: string;
  orgId?: string;
  name?: string;
};

type FormValues = {
  handle: string;
  password: string;
  loginID: string;
};

function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debouncedValue;
}

function HandleCombobox({
  value,
  onValueChange,
}: {
  value: string;
  onValueChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [searchValue, setSearchValue] = useState(value || "");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const inputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setSearchValue(value || "");
  }, [value]);

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedSearchValue],
    queryFn: () => searchActorsTypeahead(debouncedSearchValue),
    enabled: !!debouncedSearchValue.trim(),
  });

  return (
    <Combobox
      items={suggestions}
      open={open}
      onOpenChange={setOpen}
      onValueChange={(actor: Actor | null) => {
        if (actor?.handle) {
          onValueChange(actor.handle);
          setSearchValue(actor.handle);
          setOpen(false);
        }
      }}
    >
      <div ref={inputRef}>
        <Input
          placeholder="alice.bsky.social"
          value={searchValue}
          onChange={(e) => {
            setSearchValue(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
        />
      </div>
      <ComboboxContent anchor={inputRef}>
        <ComboboxEmpty>No results found.</ComboboxEmpty>
        <ComboboxList>
          {(item: Actor) => (
            <ComboboxItem key={item.handle} value={item}>
              <UserAvatar actor={item} size="sm" />
              {item.displayName || item.handle}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function JoinPage() {
  const { token } = Route.useSearch();
  const [result, setResult] = useState<{ handle: string; did: string } | null>(null);

  const payload = decodeJwtPayload(token) as InviteTokenPayload | null;
  const loginMethod = payload?.loginMethod ?? "password";
  const handleSubdomain = payload?.handleSubdomain ?? "";
  const orgId = payload?.orgId ?? "";
  const orgName = payload?.name ?? handleSubdomain;

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

  if (!payload) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <p className="text-muted-foreground">Invalid invite token.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">
        Join {orgName}
      </h1>
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
        ) : (
          <Field>
            <FieldLabel>
              {loginMethod === "atproto" ? "AT Protocol Handle" : "Google Email"}
            </FieldLabel>
            <Controller
              control={control}
              name="loginID"
              rules={{ required: true }}
              render={({ field: { onChange, value } }) =>
                loginMethod === "atproto" ? (
                  <HandleCombobox value={value ?? ""} onValueChange={onChange} />
                ) : (
                  <Input
                    placeholder="user@gmail.com"
                    value={value ?? ""}
                    onChange={(e) => onChange(e.target.value)}
                  />
                )
              }
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
