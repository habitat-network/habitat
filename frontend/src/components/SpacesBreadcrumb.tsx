import { Fragment, type ReactElement, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { DidHoverCard } from "@/components/DidHoverCard";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "internal/components/ui";

export interface SpacesBreadcrumbProps {
  spaceOwner?: string;
  spaceType?: string;
  spaceKey?: string;
  recordOwner?: string;
  record?: { recordType: string; recordKey: string };
}

interface Crumb {
  key: string;
  label: ReactNode;
  mono?: boolean;
  // Absent on the deepest crumb, which renders as the current page.
  link?: ReactElement;
}

export function SpacesBreadcrumb({
  spaceOwner,
  spaceType,
  spaceKey,
  recordOwner,
  record,
}: SpacesBreadcrumbProps) {
  const crumbs: Crumb[] = [
    { key: "root", label: "Spaces", link: <Link to="/spaces">Spaces</Link> },
  ];

  if (spaceOwner) {
    const owner = (
      <DidHoverCard did={spaceOwner} className="font-mono">
        {spaceOwner}
      </DidHoverCard>
    );
    crumbs.push({
      key: "owner",
      label: owner,
      mono: true,
      link: (
        <DidHoverCard did={spaceOwner}>
          <Link
            to="/spaces/$spaceOwner"
            params={{ spaceOwner }}
            title={spaceOwner}
          >
            {spaceOwner}
          </Link>
        </DidHoverCard>
      ),
    });
  }

  if (spaceType) {
    crumbs.push({
      key: "type",
      label: spaceType,
      mono: true,
      link: spaceOwner ? (
        <Link
          to="/spaces/$spaceOwner/$spaceType"
          params={{ spaceOwner, spaceType }}
        >
          {spaceType}
        </Link>
      ) : (
        <Link to="/spaces/type/$spaceType" params={{ spaceType }}>
          {spaceType}
        </Link>
      ),
    });
  }

  if (spaceOwner && spaceType && spaceKey) {
    crumbs.push({
      key: "space",
      label: spaceKey,
      mono: true,
      link: (
        <Link
          to="/spaces/$spaceOwner/$spaceType/$spaceKey"
          params={{ spaceOwner, spaceType, spaceKey }}
        >
          {spaceKey}
        </Link>
      ),
    });

    if (recordOwner) {
      const owner = (
        <DidHoverCard did={recordOwner} className="font-mono">
          {recordOwner}
        </DidHoverCard>
      );
      crumbs.push({
        key: "recordOwner",
        label: owner,
        mono: true,
        link: (
          <DidHoverCard did={recordOwner}>
            <Link
              to="/spaces/$spaceOwner/$spaceType/$spaceKey/$recordOwner"
              params={{ spaceOwner, spaceType, spaceKey, recordOwner }}
              title={recordOwner}
            >
              {recordOwner}
            </Link>
          </DidHoverCard>
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
