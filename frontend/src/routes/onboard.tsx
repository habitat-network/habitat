import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { HandleResolver, DidResolver, DidDocument } from "@atproto/identity";
import { AtpAgent } from "@atproto/api";
import { useState } from "react";
import confetti from "canvas-confetti";

export const habitatServers = ["https://habitat-953995456319.us-west1.run.app"];

interface IntermediateState {
  agent: AtpAgent;
  didDoc: DidDocument;
}

export const Route = createFileRoute("/onboard")({
  component() {
    const [step, setStep] = useState(1);
    const [intermediateState, setIntermediateState] =
      useState<IntermediateState>();

    return (
      <>
        <h1>Onboard</h1>
        <Step1
          onIntermediateState={setIntermediateState}
          onNextStep={() => setStep(2)}
          active={step === 1}
        />
        {<Step2 intermediateState={intermediateState} active={step === 2} />}
      </>
    );
  },
});

const Step1 = ({
  onIntermediateState,
  onNextStep,
  active,
}: {
  onIntermediateState: (data: IntermediateState) => void;
  onNextStep: () => void;
  active: boolean;
}) => {
  interface FormData {
    handle: string;
    password: string;
  }
  const form = useForm<FormData>();
  const { mutate: requestPlcOperation, isPending } = useMutation({
    mutationFn: async (data: FormData) => {
      const handleResolver = new HandleResolver();
      const did = await handleResolver.resolve(data.handle);
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
          <input
            {...form.register("handle")}
            required
            defaultValue={"sashankg.bsky.social"}
          />
        </label>
        <label>
          Password
          <input
            {...form.register("password")}
            required
            type="password"
            defaultValue={"6lSJVTChdGKAww"}
          />
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
}: {
  intermediateState: IntermediateState | undefined;
  active: boolean;
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
      services["habitat"] = {
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
          <select {...form.register("server")} required>
            {habitatServers.map((server) => (
              <option key={server} value={server}>
                {server}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" aria-busy={isPending}>
          Update identity
        </button>
      </fieldset>
    </form>
  );
};
