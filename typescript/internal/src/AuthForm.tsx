import type { AuthManager } from "./authManager";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import { useId } from "react";
import { Button, Card, CardContent, CardHeader, CardTitle, FieldError, FieldLegend, FieldSet, FieldTitle, Input } from "./components/ui";
import { Field, FieldGroup, FieldLabel } from "./components/ui/field";
import { truthy } from "./utils/arrays";

interface AuthFormData {
  handle: string;
}

interface AuthFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  serverError?: string;
  defaultHandle?: string;
  orgLoginUrl?: string;
}

export default function AuthForm({
  authManager,
  redirectUrl,
  serverError,
  defaultHandle,
  orgLoginUrl,
}: AuthFormProps) {
  const { register, handleSubmit, formState: { errors } } = useForm<AuthFormData>();
  const {
    mutate: login,
    isPending,
    error,
  } = useMutation({
    async mutationFn({ handle }: AuthFormData) {
      const url = authManager.loginUrl(handle, redirectUrl);
      window.location.href = url.toString();
      await new Promise(() => { })
    },
  });
  return (
    <div className="flex items-center justify-center py-32">
      <Card className="w-full max-w-sm">
        <CardContent>
          <form onSubmit={handleSubmit((data) => login(data))}>
            <FieldSet>
              <FieldLegend>
                Sign In
              </FieldLegend>
              <FieldGroup>
                <Field>
                  <FieldLabel>Handle</FieldLabel>
                  <Input
                    {...register("handle", { required: "Handle is required" })}
                    defaultValue={defaultHandle}
                    placeholder="alice.bsky.social"
                  />
                  <FieldError errors={[error, { message: serverError }, errors.handle].filter(truthy)} />
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
