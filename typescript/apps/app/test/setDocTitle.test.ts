import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import { setDocTitle } from "@/hooks/useYDoc";

const ORG = "did:web:org.example";
const doc = (docId: string, title: string) => ({
  docId,
  uri: docId,
  ownerDid: ORG,
  title,
});

it("updates the doc page's summary as well as the doc list", () => {
  const qc = new QueryClient();
  qc.setQueryData(["docs", ORG], [doc("a", "Old"), doc("b", "Other")]);
  qc.setQueryData(["docSummary", ORG, "a"], doc("a", "Old"));
  qc.setQueryData(["docSummary", ORG, "b"], doc("b", "Other"));

  setDocTitle(qc, "a", "New");

  expect(
    qc.getQueryData<{ title: string }>(["docSummary", ORG, "a"])?.title,
  ).toBe("New");
  expect(
    qc.getQueryData<{ title: string }>(["docSummary", ORG, "b"])?.title,
  ).toBe("Other");
  expect(
    qc.getQueryData<{ title: string }[]>(["docs", ORG])?.map((d) => d.title),
  ).toEqual(["New", "Other"]);
});
