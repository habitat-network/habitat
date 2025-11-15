import React from "react";
import type { ReverseProxyRule } from "types/node";

interface ReverseProxyRuleListProps {
  rules: ReverseProxyRule[];
}

const ReverseProxyRuleList: React.FC<ReverseProxyRuleListProps> = ({
  rules,
}) => {
  return (
    <div className="reverse-proxy-rule-list">
      <h2 className="text-xl font-bold mb-4">Reverse Proxy Rules</h2>
      {rules.length === 0 ? (
        <p>No reverse proxy rules found.</p>
      ) : (
        <ul className="space-y-4">
          {Object.entries(rules).map(([key, rule]) => (
            <li key={key} className="bg-white p-4 rounded-lg shadow">
              <div className="font-semibold">
                <a href={rule.matcher} className="text-blue-700">
                  Rule: {key}
                </a>
              </div>
              <div>Type: {rule.type}</div>
              <div>Target: {rule.target}</div>
              <div>Matcher: {rule.matcher}</div>
              {rule.app_id && <div>App ID: {rule.app_id}</div>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

export default ReverseProxyRuleList;
