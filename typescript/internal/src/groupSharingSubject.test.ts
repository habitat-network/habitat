import { expect, it } from "vitest";
import { parseGroupSharingSubject } from "./groupSharingSubject";

const ORG = "did:web:org.example";

it("recognizes both namespaces without changing the group DID", () => {
  const legacy = parseGroupSharingSubject(
    `at:space/${ORG}/community.opensocial.members/self`,
  );
  const canonical = parseGroupSharingSubject(
    `at:space/${ORG}/org.atgrp.members/self`,
  );
  expect(legacy).toEqual({ kind: "legacy", orgDid: ORG });
  expect(canonical).toEqual({ kind: "canonical", orgDid: ORG });
});

it("preserves the legacy classification for all legacy members-space keys", () => {
  expect(
    parseGroupSharingSubject(
      `at:space/${ORG}/community.opensocial.members/old`,
    ),
  ).toEqual({ kind: "legacy", orgDid: ORG });
});

it.each([
  ORG,
  `at:space/${ORG}/org.atgrp.members/other`,
  `at:space/${ORG}/org.atgrp.meta/self`,
  `at:space/${ORG}/org.atgrp.members/self/${ORG}/org.atgrp.role/admin`,
])("does not recognize %s as a group-sharing subject", (subject) => {
  expect(parseGroupSharingSubject(subject)).toBeUndefined();
});
