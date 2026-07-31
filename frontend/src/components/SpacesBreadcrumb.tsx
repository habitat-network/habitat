import { Fragment, type ReactElement } from "react";
import { Link } from "@tanstack/react-router";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "internal/components/ui";
import type { SpaceURIParts } from "internal";

// shortenDid keeps a DID recognizable in a breadcrumb without letting it push
// the rest of the trail off the line.
export function shortenDid(did: string): string {
  return did.length > 24 ? `${did.slice(0, 12)}…${did.slice(-8)}` : did;
}

export interface SpacesBreadcrumbProps {
  // Each level the current page has drilled into. The deepest one provided is
  // rendered as the current page; the rest link back up the chain.
  spaceType?: string;
  space?: SpaceURIParts;
  recordOwner?: string;
  record?: { recordType: string; recordKey: string };
}

interface Crumb {
  key: string;
  label: string;
  mono?: boolean;
  // Absent on the deepest crumb, which renders as the current page.
  link?: ReactElement;
}

export function SpacesBreadcrumb({
  spaceType,
  space,
  recordOwner,
  record,
}: SpacesBreadcrumbProps) {
  const crumbs: Crumb[] = [
    { key: "root", label: "Spaces", link: <Link to="/spaces">Spaces</Link> },
  ];

  const type = space?.spaceType ?? spaceType;
  if (type) {
    crumbs.push({
      key: "type",
      label: type,
      mono: true,
      link: (
        <Link to="/spaces/type/$spaceType" params={{ spaceType: type }}>
          {type}
        </Link>
      ),
    });
  }

  if (space) {
    crumbs.push({
      key: "space",
      label: space.spaceKey,
      mono: true,
      link: (
        <Link to="/spaces/$spaceOwner/$spaceType/$spaceKey" params={space}>
          {space.spaceKey}
        </Link>
      ),
    });

    if (recordOwner) {
      crumbs.push({
        key: "recordOwner",
        label: shortenDid(recordOwner),
        mono: true,
        link: (
          <Link
            to="/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner"
            params={{ ...space, recordOwner }}
            title={recordOwner}
          >
            {shortenDid(recordOwner)}
          </Link>
        ),
      });

      if (record) {
        crumbs.push({
          key: "record",
          label: `${record.recordType}/${record.recordKey}`,
          mono: true,
        });
      }
    }
  }

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {crumbs.map((crumb, i) => {
          const isLast = i === crumbs.length - 1;
          const className = crumb.mono ? "font-mono" : undefined;
          return (
            <Fragment key={crumb.key}>
              {i > 0 && <BreadcrumbSeparator />}
              <BreadcrumbItem>
                {isLast || !crumb.link ? (
                  <BreadcrumbPage className={className}>
                    {crumb.label}
                  </BreadcrumbPage>
                ) : (
                  <BreadcrumbLink className={className} render={crumb.link} />
                )}
              </BreadcrumbItem>
            </Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
