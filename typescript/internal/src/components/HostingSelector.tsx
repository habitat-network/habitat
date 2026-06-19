import { useEffect, useState } from "react";
import { Field, FieldLabel } from "./ui/field";
import { Input } from "./ui/input";
import { ToggleGroup, ToggleGroupItem } from "./ui/toggle-group";
import { describeInstance } from "./instanceQueries";

interface HostingSelectorProps {
  defaultDomain: string;
  value: string;
  onChange: (domain: string) => void;
  disabled?: boolean;
}

export function HostingSelector({
  defaultDomain,
  value,
  onChange,
  disabled,
}: HostingSelectorProps) {
  const [useCustomInstance, setUseCustomInstance] = useState(
    value !== defaultDomain,
  );
  const [customDomain, setCustomDomain] = useState(
    value !== defaultDomain ? value : "",
  );
  const [customInstanceName, setCustomInstanceName] = useState<string | null>(
    null,
  );
  const [customInstanceError, setCustomInstanceError] = useState<
    string | null
  >(null);

  useEffect(() => {
    if (!useCustomInstance || !customDomain) {
      setCustomInstanceName(null);
      setCustomInstanceError(null);
      return;
    }
    let cancelled = false;
    describeInstance(customDomain)
      .then((result) => {
        if (!cancelled) {
          setCustomInstanceName(result.name);
          setCustomInstanceError(null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCustomInstanceName(null);
          setCustomInstanceError("Could not reach that instance.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [useCustomInstance, customDomain]);

  return (
    <Field>
      <FieldLabel>Hosting</FieldLabel>
      <ToggleGroup
        variant="outline"
        value={[useCustomInstance ? "custom" : "managed"]}
        onValueChange={(v) => {
          const next = v[0] === "custom";
          setUseCustomInstance(next);
          onChange(next ? customDomain : defaultDomain);
        }}
        disabled={disabled}
      >
        <ToggleGroupItem value="managed">
          Managed hosting by Habitat
        </ToggleGroupItem>
        <ToggleGroupItem value="custom">Custom instance</ToggleGroupItem>
      </ToggleGroup>
      {useCustomInstance ? (
        <>
          <Input
            placeholder="myinstance.example.com"
            value={customDomain}
            disabled={disabled}
            onChange={(e) => {
              setCustomDomain(e.target.value);
              onChange(e.target.value);
            }}
          />
          {customInstanceName ? (
            <Input value={customInstanceName} disabled />
          ) : null}
          {customInstanceError ? (
            <p className="text-sm text-destructive">{customInstanceError}</p>
          ) : null}
        </>
      ) : null}
    </Field>
  );
}
