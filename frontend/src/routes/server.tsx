import { getNode } from "@/api/node";
import AppList from "@/components/appList";
import ReverseProxyRuleList from "@/components/reverseProxyRuleList";
import { createFileRoute } from "@tanstack/react-router";
import type { AppInstallation, Process, ReverseProxyRule } from "types/node";

export const Route = createFileRoute("/server")({
  async loader() {
    const node = await getNode();
    return {
      apps: Object.values(node!.state.app_installations).filter(
        (app) => app !== undefined,
      ) as AppInstallation[],
      processes: Object.values(node!.state.processes).filter(
        (process) => process !== undefined,
      ) as Process[],
      proxyRules: Object.values(node!.state.reverse_proxy_rules || {}).filter(
        (rule) => rule !== undefined,
      ) as ReverseProxyRule[],
    };
  },
  component() {
    const { apps, processes, proxyRules } = Route.useLoaderData();
    return (
      <>
        <h1>Habitat Node State</h1>
        <details>
          <summary>Apps</summary>
          <AppList
            apps={apps}
            processes={processes}
            reverseProxyRules={proxyRules}
          />
        </details>
        <hr />
        <details>
          <summary>Reverse Proxy Rules</summary>
          <ReverseProxyRuleList rules={proxyRules} />
        </details>
        <hr />
        <details>
          <summary>Users</summary>
          <p>User content goes here</p>
        </details>
      </>
    );
  },
});
