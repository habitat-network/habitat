import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { HandleResolver, DidResolver, DidDocument } from "@atproto/identity";
import { AtpAgent } from "@atproto/api";
import { useState } from "react";
import confetti from "canvas-confetti";

// TODO: can this point to a habitat.network url instead?
export const habitatServers = ["https://habitat-953995456319.us-west1.run.app"];

interface IntermediateState {
  agent: AtpAgent;
  didDoc: DidDocument;
}

export interface OnboardProps {
  serviceKey?: string;
  title?: string;
  serverOptions?: string[];
  defaultServer?: string;
  handle?: string;
}

export function OnboardComponent({
  serviceKey = "habitat",
  title = "Onboard",
  serverOptions,
  defaultServer,
  handle,
}: OnboardProps = {}) {
  const [step, setStep] = useState(1);
  const [intermediateState, setIntermediateState] =
    useState<IntermediateState>();

  return (
    <>
      <h1>{title}</h1>
      <p>To use habitat, we need to onboard you to this service by adding a new field to your DID doc. This adds the habitat service and does not disrupt any existing services you have or your PDS. We need your password to make a signed operation, but this runs completely client-side and we don't store your password anywhere.</p>
      <Step1
        onIntermediateState={setIntermediateState}
        onNextStep={() => setStep(2)}
        active={step === 1}
        defaultHandle={handle}
      />
      <Step2
        intermediateState={intermediateState}
        active={step === 2}
        serviceKey={serviceKey}
        serverOptions={serverOptions}
        defaultServer={defaultServer}
      />
    </>
  );
}

export const Route = createFileRoute("/onboard")({
  component: () => <OnboardComponent serverOptions={habitatServers} />,
});

const Step1 = ({
  onIntermediateState,
  onNextStep,
  active,
  defaultHandle,
}: {
  onIntermediateState: (data: IntermediateState) => void;
  onNextStep: () => void;
  active: boolean;
  defaultHandle?: string;
}) => {
  interface FormData {
    handle: string;
    password: string;
  }
  const form = useForm<FormData>({
    defaultValues: { handle: defaultHandle ?? "you.bsky.social" },
  });
  const { mutate: requestPlcOperation, isPending } = useMutation({
    mutationFn: async (data: FormData) => {
      const handleResolver = new HandleResolver();
      const did = await handleResolver.resolve(data.handle);
      console.log("did", did)
      if (!did) throw new Error("Handle not found");

      const didResolver = new DidResolver({});
      const didDoc = await didResolver.resolve(did);
      const atprotoData = await didResolver.resolveAtprotoData(did);
      if (!didDoc || !atprotoData) throw new Error("Document not found");

      const agent = new AtpAgent({
        service: atprotoData.pds,
      });
      await agent.login({
        identifier: data.handle,
        password: data.password,
      });
      const response = await agent.call(
        "com.atproto.identity.requestPlcOperationSignature",
      );
      if (!response.success) throw new Error(response.data);
      onIntermediateState({
        agent,
        didDoc,
      });
    },
    onSuccess: () => {
      onNextStep();
    },
    onError: (error) => {
      alert(error.message);
    },
  });
  return (
    <form onSubmit={form.handleSubmit((data) => requestPlcOperation(data))}>
      <fieldset disabled={!active}>
        <legend>
          <h2>Step 1</h2>
        </legend>
        <label>
          Handle
          <input {...form.register("handle")} required />
        </label>
        <label>
          Password
          <input {...form.register("password")} required type="password" />
        </label>
        <button type="submit" aria-busy={isPending}>
          Request code for identity update
        </button>
      </fieldset>
    </form>
  );
};

const Step2 = ({
  intermediateState,
  active,
  serviceKey,
  serverOptions,
  defaultServer,
}: {
  intermediateState: IntermediateState | undefined;
  active: boolean;
  serviceKey: string;
  serverOptions?: string[];
  defaultServer?: string;
}) => {
  interface FormData {
    code: string;
    server: string;
  }
  const form = useForm<FormData>();
  const { mutate: updateIdentity, isPending } = useMutation({
    mutationFn: async (data: FormData) => {
      if (!intermediateState) throw new Error("Intermediate state not found");
      const services: Record<
        string,
        { type: string; endpoint: string | Record<string, unknown> }
      > = {};
      for (const service of intermediateState.didDoc.service ?? []) {
        services[service.id.slice(1) /* remove leading # */] = {
          type: service.type,
          endpoint: service.serviceEndpoint,
        };
      }
      services[serviceKey] = {
        type: "HabitatServer",
        endpoint: data.server,
      };
      const signResponse = await intermediateState.agent.call(
        "com.atproto.identity.signPlcOperation",
        undefined,
        {
          token: data.code,
          services,
        },
      );
      if (!signResponse.success) throw new Error(signResponse.data);
      const submitResponse = await intermediateState.agent.call(
        "com.atproto.identity.submitPlcOperation",
        undefined,
        {
          operation: signResponse.data.operation,
        },
      );
      if (!submitResponse.success) throw new Error(submitResponse.data);
    },
    onSuccess: () => {
      confetti();
      window.location.assign("/");
    },
    onError: (error) => {
      alert(error.message);
    },
  });
  return (
    <form onSubmit={form.handleSubmit((data) => updateIdentity(data))}>
      <fieldset disabled={!active}>
        <legend>
          <h2>Step 2</h2>
        </legend>
        <label>
          Code
          <input {...form.register("code")} required />
        </label>
        <label>
          Server
          {serverOptions ? (
            <select {...form.register("server")} required>
              {serverOptions.map((server) => (
                <option key={server} value={server}>
                  {server}
                </option>
              ))}
            </select>
          ) : (
            <input
              {...form.register("server")}
              required
              defaultValue={defaultServer}
            />
          )}
        </label>
        <button type="submit" aria-busy={isPending}>
          Update identity
        </button>
      </fieldset>
    </form>
  );
};
