import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

export const Route = createFileRoute("/_requireAuth/blob-test/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { authManager } = Route.useRouteContext();

  const [file, setFile] = useState<File | null>(null);
  const [loading, setLoading] = useState(false);
  const [response, setResponse] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);

  const [cid, setCid] = useState("");
  const [getBlobLoading, setGetBlobLoading] = useState(false);
  const [getBlobResponse, setGetBlobResponse] = useState<any>(null);
  const [getBlobError, setGetBlobError] = useState<string | null>(null);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files?.[0]) {
      setFile(e.target.files[0]);
      setError(null);
      setResponse(null);
    }
  };

  const handleUpload = async () => {
    if (!file) {
      setError("Please select a file");
      return;
    }

    setLoading(true);
    setError(null);
    setResponse(null);

    try {
      const buf = await file.arrayBuffer();
      const headers = new Headers();
      headers.append("Content-Type", file.type || "application/octet-stream");
      const res = await authManager.fetch(
        "/xrpc/network.habitat.uploadBlob",
        "POST",
        buf,
        headers,
      );

      if (!res) {
        throw new Error(`Upload failed: no response`);
      } else if (!res.ok) {
        const errorText = await res.text();
        throw new Error(`Upload failed: ${res.status} ${errorText}`);
      }

      const data = await res.json();
      setResponse(data);
      setFile(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  };

  const handleGetBlob = async () => {
    if (!cid) {
      setGetBlobError("Please paste a CID");
      return;
    }

    setGetBlobLoading(true);
    setGetBlobError(null);
    setGetBlobResponse(null);

    try {
      const res = await authManager.fetch(
        `/xrpc/network.habitat.getBlob?cid=${encodeURIComponent(cid)}&did=${authManager.handle}`,
        "GET",
      );

      if (!res) {
        throw new Error(`Get blob failed: no response`);
      } else if (!res.ok) {
        const errorText = await res.text();
        throw new Error(`Get blob failed: ${res.status} ${errorText}`);
      }

      const contentType = res.headers.get("content-type");

      if (contentType?.includes("application/json")) {
        const data = await res.json();
        setGetBlobResponse(data);
      } else if (
        contentType?.includes("image/png") ||
        contentType?.startsWith("image/")
      ) {
        const blob = await res.blob();
        setGetBlobResponse({
          blob: blob,
          size: blob.size,
          type: contentType || "application/octet-stream",
          url: URL.createObjectURL(blob),
        });
      } else {
        const blob = await res.blob();
        setGetBlobResponse({
          blob: blob,
          size: blob.size,
          type: contentType || "application/octet-stream",
          url: URL.createObjectURL(blob),
        });
      }
    } catch (err) {
      setGetBlobError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setGetBlobLoading(false);
    }
  };

  return (
    <div style={{ padding: "20px" }}>
      <h1>Blob Upload Test</h1>

      <div style={{ marginBottom: "20px" }}>
        <input
          type="file"
          onChange={handleFileChange}
          disabled={loading}
          style={{ marginRight: "10px" }}
        />
        <button
          onClick={handleUpload}
          disabled={loading || !file}
          style={{
            padding: "8px 16px",
            backgroundColor: loading || !file ? "#ccc" : "#007bff",
            color: "white",
            border: "none",
            borderRadius: "4px",
            cursor: loading || !file ? "not-allowed" : "pointer",
          }}
        >
          {loading ? "Uploading..." : "Upload"}
        </button>
      </div>

      {file && (
        <div style={{ marginBottom: "10px", fontSize: "14px", color: "#666" }}>
          Selected: {file.name} ({(file.size / 1024).toFixed(2)} KB)
        </div>
      )}

      {error && (
        <div
          style={{
            padding: "10px",
            marginBottom: "10px",
            backgroundColor: "#f8d7da",
            color: "#721c24",
            borderRadius: "4px",
          }}
        >
          {error}
        </div>
      )}

      {response && (
        <div
          style={{
            padding: "10px",
            backgroundColor: "#d4edda",
            color: "#155724",
            borderRadius: "4px",
          }}
        >
          <h3>Upload Successful!</h3>
          <pre style={{ overflow: "auto" }}>
            {JSON.stringify(response, null, 2)}
          </pre>
        </div>
      )}

      <hr style={{ margin: "40px 0" }} />

      <h2>Get Blob</h2>

      <div style={{ marginBottom: "20px" }}>
        <textarea
          value={cid}
          onChange={(e) => {
            setCid(e.target.value);
            setGetBlobError(null);
            setGetBlobResponse(null);
          }}
          placeholder="Paste CID here..."
          disabled={getBlobLoading}
          style={{
            width: "100%",
            height: "60px",
            padding: "8px",
            marginBottom: "10px",
            fontFamily: "monospace",
            fontSize: "14px",
            border: "1px solid #ccc",
            borderRadius: "4px",
          }}
        />
        <button
          onClick={handleGetBlob}
          disabled={getBlobLoading || !cid}
          style={{
            padding: "8px 16px",
            backgroundColor: getBlobLoading || !cid ? "#ccc" : "#28a745",
            color: "white",
            border: "none",
            borderRadius: "4px",
            cursor: getBlobLoading || !cid ? "not-allowed" : "pointer",
          }}
        >
          {getBlobLoading ? "Fetching..." : "Get Blob"}
        </button>
      </div>

      {getBlobError && (
        <div
          style={{
            padding: "10px",
            marginBottom: "10px",
            backgroundColor: "#f8d7da",
            color: "#721c24",
            borderRadius: "4px",
          }}
        >
          {getBlobError}
        </div>
      )}

      {getBlobResponse && (
        <div
          style={{
            padding: "10px",
            backgroundColor: "#d4edda",
            color: "#155724",
            borderRadius: "4px",
          }}
        >
          <h3>Blob Retrieved!</h3>
          {getBlobResponse.url ? (
            <div>
              <p>
                <strong>Type:</strong> {getBlobResponse.type}
              </p>
              <p>
                <strong>Size:</strong>{" "}
                {(getBlobResponse.size / 1024).toFixed(2)} KB
              </p>
              {getBlobResponse.type.startsWith("image/") && (
                <img
                  src={getBlobResponse.url}
                  alt="Retrieved blob"
                  style={{ maxWidth: "100%", maxHeight: "400px" }}
                />
              )}
              {getBlobResponse.type.startsWith("text/") && (
                <pre
                  style={{
                    overflow: "auto",
                    backgroundColor: "#f5f5f5",
                    padding: "10px",
                  }}
                >
                  {/* Note: actual text content would need to be fetched differently */}
                  [Binary content - {getBlobResponse.size} bytes]
                </pre>
              )}
              {!getBlobResponse.type.startsWith("image/") &&
                !getBlobResponse.type.startsWith("text/") && (
                  <div>
                    <a href={getBlobResponse.url} download="blob">
                      Download Blob
                    </a>
                  </div>
                )}
            </div>
          ) : (
            <pre style={{ overflow: "auto" }}>
              {JSON.stringify(getBlobResponse, null, 2)}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}
