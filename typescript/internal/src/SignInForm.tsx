import { useForm } from "react-hook-form";
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
  "Your email's domain isn't set up for Habitat sign-in. Ask your organization's admin, or sign in with your ATProto handle.";

interface SignInFormData {
  loginHint: string;
}

interface SignInFormProps {
  // Starts sign-in for a handle or work email; a thrown error's message is
  // shown under the input. The server routes a handle to the user's PDS and a
  // work email to its email domain's login provider.
  onSubmit: (loginHint: string) => Promise<void>;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
}

export default function SignInForm({
  onSubmit,
  serverError,
  defaultHandle,
  orgLoginUrl,
}: SignInFormProps) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<SignInFormData>();
  const {
    mutate: login,
    isPending,
    error,
  } = useMutation({
    async mutationFn({ loginHint }: SignInFormData) {
      await onSubmit(loginHint.trim());
    },
  });
  return (
    <div className="flex items-center justify-center py-32">
      <Card className="w-full max-w-sm">
        <CardContent>
          <form onSubmit={handleSubmit((data) => login(data))}>
            <FieldSet>
              <FieldLegend>Sign In</FieldLegend>
              <FieldGroup>
                <Field>
                  <FieldLabel>atproto handle or work email</FieldLabel>
                  <Input
                    {...register("loginHint", {
                      required: "Handle or email is required",
                    })}
                    defaultValue={defaultHandle}
                    placeholder="alice.bsky.social or you@company.com"
                  />
                  <FieldError
                    errors={[
                      error,
                      { message: serverError },
                      errors.loginHint,
                    ].filter(truthy)}
                  />
                </Field>
                <Field>
                  <Button loading={isPending} type="submit" className="w-full">
                    Sign In
                  </Button>
                </Field>
              </FieldGroup>
            </FieldSet>
          </form>
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
