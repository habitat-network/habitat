import {
  Button,
  Card,
  CardContent,
  Field,
  FieldError,
  FieldLabel,
  Input,
} from "internal/components/ui";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { XRPCError } from "internal";

export const Route = createFileRoute("/_requireAuth/blob-test/")({
  component: RouteComponent,
});

interface UploadFormValues {
  file: FileList | null;
}

interface GetBlobFormValues {
  space: string;
  cid: string;
}

interface FetchedBlob {
  size: number;
  type: string;
  url: string;
}

function RouteComponent() {
  const { authManager } = Route.useRouteContext();

  const uploadForm = useForm<UploadFormValues>({
    defaultValues: { file: null },
  });

  const {
    mutate: upload,
    isPending: uploading,
    isSuccess: uploadSucceeded,
    data: uploadResponse,
  } = useMutation({
    async mutationFn(values: UploadFormValues) {
      const file = values.file?.[0];
      if (!file) {
        throw new Error("Please select a file");
      }

      const buf = await file.arrayBuffer();
      const headers = new Headers();
      headers.append("Content-Type", file.type || "application/octet-stream");
      const res = await authManager.fetch(
        "/xrpc/network.habitat.repo.uploadBlob",
        "POST",
        buf,
        headers,
      );

      if (!res) {
        throw new Error("Upload failed: no response");
      } else if (!res.ok) {
        const errorText = await res.text();
        throw new Error(`Upload failed: ${res.status} ${errorText}`);
      }

      return res.json();
    },
    onSuccess: () => {
      uploadForm.reset({ file: null });
    },
    onError: (err) => {
      uploadForm.setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    },
  });

  const getBlobForm = useForm<GetBlobFormValues>({
    defaultValues: { space: "", cid: "" },
  });

  const {
    mutate: getBlob,
    isPending: fetchingBlob,
    isSuccess: getBlobSucceeded,
    data: fetchedBlob,
  } = useMutation({
    async mutationFn(values: GetBlobFormValues): Promise<FetchedBlob> {
      const res = await authManager.fetch(
        `/xrpc/network.habitat.space.getBlob?space=${encodeURIComponent(values.space)}&cid=${encodeURIComponent(values.cid)}`,
        "GET",
      );

      if (!res) {
        throw new Error("Get blob failed: no response");
      } else if (!res.ok) {
        const data = await res.json().catch(() => undefined);
        throw new XRPCError(res.status, data ?? { error: "UnknownError" });
      }

      const contentType = res.headers.get("content-type");
      const blob = await res.blob();
      return {
        size: blob.size,
        type: contentType || "application/octet-stream",
        url: URL.createObjectURL(blob),
      };
    },
    onError: (err) => {
      getBlobForm.setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    },
  });

  return (
    <div className="flex flex-col gap-8 p-6">
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-semibold">Blob Upload Test</h1>
        <form
          onSubmit={uploadForm.handleSubmit((values) => upload(values))}
          className="flex flex-col gap-4"
        >
          <fieldset disabled={uploading} className="flex flex-col gap-4">
            <Field>
              <FieldLabel>File</FieldLabel>
              <Input type="file" {...uploadForm.register("file")} />
              <FieldError errors={[uploadForm.formState.errors.file]} />
            </Field>
            <FieldError errors={[uploadForm.formState.errors.root]} />
            <Button type="submit" className="w-fit">
              {uploading ? "Uploading..." : "Upload"}
            </Button>
          </fieldset>
        </form>
        {uploadSucceeded && (
          <Card>
            <CardContent>
              <h3 className="font-medium mb-2">Upload Successful!</h3>
              <pre className="overflow-auto text-sm">
                {JSON.stringify(uploadResponse, null, 2)}
              </pre>
            </CardContent>
          </Card>
        )}
      </div>

      <div className="flex flex-col gap-4">
        <h2 className="text-xl font-semibold">Get Blob</h2>
        <form
          onSubmit={getBlobForm.handleSubmit((values) => getBlob(values))}
          className="flex flex-col gap-4"
        >
          <fieldset disabled={fetchingBlob} className="flex flex-col gap-4">
            <Field>
              <FieldLabel>Space</FieldLabel>
              <Input
                placeholder="habitat://..."
                {...getBlobForm.register("space", { required: true })}
              />
              <FieldError errors={[getBlobForm.formState.errors.space]} />
            </Field>
            <Field>
              <FieldLabel>CID</FieldLabel>
              <Input
                placeholder="bafy..."
                {...getBlobForm.register("cid", { required: true })}
              />
              <FieldError errors={[getBlobForm.formState.errors.cid]} />
            </Field>
            <FieldError errors={[getBlobForm.formState.errors.root]} />
            <Button type="submit" className="w-fit">
              {fetchingBlob ? "Fetching..." : "Get Blob"}
            </Button>
          </fieldset>
        </form>

        {getBlobSucceeded && fetchedBlob && (
          <Card>
            <CardContent className="flex flex-col gap-2">
              <h3 className="font-medium">Blob Retrieved!</h3>
              <p>
                <strong>Type:</strong> {fetchedBlob.type}
              </p>
              <p>
                <strong>Size:</strong> {(fetchedBlob.size / 1024).toFixed(2)}{" "}
                KB
              </p>
              {fetchedBlob.type.startsWith("image/") ? (
                <img
                  src={fetchedBlob.url}
                  alt="Retrieved blob"
                  className="max-w-full max-h-[400px]"
                />
              ) : (
                <a href={fetchedBlob.url} download="blob">
                  Download Blob
                </a>
              )}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
