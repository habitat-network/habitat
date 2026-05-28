import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  ToggleGroup,
  ToggleGroupItem,
  Combobox,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "internal/components/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Controller, useForm } from "react-hook-form";
import { procedure, searchActorsTypeahead, UserAvatar } from "internal";
import type { Actor } from "internal";
import { NetworkHabitatOrgCreate } from "api";
import { useQuery } from "@tanstack/react-query";
import { useState, useEffect, useRef } from "react";

export const Route = createFileRoute("/org/create")({
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
  const prevLoginMethod = useRef<FormValues["login_method"]>("password");
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

  useEffect(() => {
    if (prevLoginMethod.current !== loginMethod) {
      setValue("login_id", "");
      setValue("admin_password", "");
      prevLoginMethod.current = loginMethod;
    }
  }, [loginMethod, setValue]);

  const onSubmit = async (values: FormValues) => {
    try {
      let body: NetworkHabitatOrgCreate.InputSchema = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
        handle_subdomain: values.handle_subdomain,
      };
      if (values.login_method === "password") {
        body.admin_password = values.admin_password;
      } else {
        body.login_id = values.login_id || undefined;
      }
      const { admin_handle } = await procedure(
        "network.habitat.org.create",
        body,
        { unauthenticated: true, domain: __HABITAT_DOMAIN__ },
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
                      if (newValue[0] && newValue[0] !== value) {
                        onChange(newValue[0]);
                      }
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
          ) : (
            <Field>
              <FieldLabel>
                {loginMethod === "atproto" ? "AT Protocol Handle" : "Google Email"}
              </FieldLabel>
              <Controller
                control={control}
                name="login_id"
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
