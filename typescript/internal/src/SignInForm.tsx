import { useState } from "react";
import { useForm, type Validate } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import {
  Button,
  Card,
  CardContent,
  FieldError,
  FieldLegend,
  FieldSet,
  Input,
} from "./components/ui";
import { Field, FieldGroup, FieldLabel } from "./components/ui/field";
import { truthy } from "./utils/arrays";

// Shown when a work email's domain isn't mapped to any org, so there's no
// identity to sign in as. Callers map their own "not found" failure to it.
export const EMAIL_DOMAIN_NOT_FOUND_MESSAGE =
  "Your email's domain isn't set up for Habitat sign-in. Ask your organization's admin, or sign in with AT Protocol.";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface SignInFormData {
  loginHint: string;
}

interface SignInFormProps {
  // Starts sign-in for a handle or work email; a thrown error's message is
  // shown under the input.
  onSubmit: (loginHint: string) => Promise<void>;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
}

type SignInMethod = "atproto" | "google";

// Both methods start the same sign-in; they differ only in the login hint
// collected. The server routes a handle to the user's PDS and a work email to
// its email domain's login provider (e.g. Google), so each method validates
// its input's shape to keep the route matching the button the user chose.
const methods: Record<
  SignInMethod,
  {
    label: string;
    inputLabel: string;
    placeholder: string;
    required: string;
    validate: Validate<string, SignInFormData>;
  }
> = {
  atproto: {
    label: "Sign in with AT Protocol",
    inputLabel: "Handle",
    placeholder: "alice.bsky.social",
    required: "Handle is required",
    validate: (value) =>
      !value.trim().replace(/^@/, "").includes("@") ||
      "That looks like an email. Use Sign in with Google instead.",
  },
  google: {
    label: "Sign in with Google",
    inputLabel: "Work email",
    placeholder: "you@company.com",
    required: "Email is required",
    validate: (value) =>
      EMAIL_PATTERN.test(value.trim()) ||
      "Enter your work email, e.g. you@company.com",
  },
};

export default function SignInForm({
  onSubmit,
  serverError,
  defaultHandle,
  orgLoginUrl,
}: SignInFormProps) {
  const [method, setMethod] = useState<SignInMethod | undefined>(
    defaultHandle ? "atproto" : undefined,
  );
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<SignInFormData>();
  const {
    mutate: login,
    isPending,
    error,
    reset: resetMutation,
  } = useMutation({
    async mutationFn({ loginHint }: SignInFormData) {
      await onSubmit(loginHint.trim());
    },
  });

  const selectMethod = (next: SignInMethod | undefined) => {
    reset({
      loginHint: next === "atproto" && defaultHandle ? defaultHandle : "",
    });
    resetMutation();
    setMethod(next);
  };

  const fieldErrors = [error, { message: serverError }, errors.loginHint];
  return (
    <div className="flex items-center justify-center py-32">
      <Card className="w-full max-w-sm">
        <CardContent>
          {method ? (
            <form onSubmit={handleSubmit((data) => login(data))}>
              <FieldSet>
                <FieldLegend>{methods[method].label}</FieldLegend>
                <FieldGroup>
                  <Field>
                    <FieldLabel>{methods[method].inputLabel}</FieldLabel>
                    <Input
                      {...register("loginHint", {
                        required: methods[method].required,
                        validate: methods[method].validate,
                      })}
                      defaultValue={
                        method === "atproto" ? defaultHandle : undefined
                      }
                      placeholder={methods[method].placeholder}
                      autoFocus
                    />
                    <FieldError errors={fieldErrors.filter(truthy)} />
                  </Field>
                  <Field>
                    <Button
                      loading={isPending}
                      type="submit"
                      className="w-full"
                    >
                      Continue
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      className="w-full"
                      onClick={() => selectMethod(undefined)}
                    >
                      Back
                    </Button>
                  </Field>
                </FieldGroup>
              </FieldSet>
            </form>
          ) : (
            <FieldSet>
              <FieldLegend>Sign In</FieldLegend>
              <FieldGroup>
                <Field>
                  {(Object.keys(methods) as SignInMethod[]).map((m) => (
                    <Button
                      key={m}
                      type="button"
                      variant="outline"
                      className="w-full"
                      onClick={() => selectMethod(m)}
                    >
                      {methods[m].label}
                    </Button>
                  ))}
                  <FieldError errors={[{ message: serverError }]} />
                </Field>
              </FieldGroup>
            </FieldSet>
          )}
          {orgLoginUrl && (
            <Button
              variant="link"
              className="mt-6"
              size="sm"
              render={<a href={orgLoginUrl} />}
            >
              Add this app to your organization
            </Button>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
