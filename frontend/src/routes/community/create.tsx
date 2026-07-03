import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { useState } from "react";
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
  const [step, setStep] = useState<1 | 2>(1);
  const [subdomainTouched, setSubdomainTouched] = useState(false);

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    trigger,
    formState: { isSubmitting, errors },
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

  const name = watch("name");

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

  // Wired up in Task 3.
  const onSubmit = async (_values: FormValues) => {};

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
                  value={name}
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
            <p>Step 2 placeholder — replaced in Task 3.</p>
          )}
          <FieldError errors={[errors.root]} />
        </fieldset>
      </form>
    </div>
  );
}
