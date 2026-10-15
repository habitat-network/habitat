# Competitive Research — Warehouse Ops Tools

Author: Leah Brandt (Product). Last updated 2026-10-03.

## Summary
The market splits into inventory-first WMS add-ons and a handful of point
solutions for floor communication. Nobody owns exceptions and handoffs end to
end. Our wedge is the shared record, not the dashboard.

## DockPilot
- Point solution for dock scheduling and floor messaging.
- Single-tenant deployments; 8–12 week onboarding.
- **Raised list prices about 40% in August 2026**, which pushed two of their
  mid-size customers (including Pine Ridge Fulfillment) to look at us.
- Strong with large single-site warehouses, weak at multi-site rollups.
- Weakness: no searchable history; messages expire after 90 days.

## Stackyard
- Lightweight task app for warehouse teams, free tier, popular with small sites.
- Only supports a single site per account, so 3PLs with several warehouses
  outgrow it.
- Shipping an "AI shift summary" feature in beta; quality is poor in our tests
  (summaries miss most exceptions).

## Legacy WMS add-ons
- Every major WMS vendor sells a "labor management" or "floor insights" module.
- Inventory-first: great at counts, blind to the conversations around them.
- Sold top-down and bundled, so pricing looks free but switching costs are high.

## Where we win
1. Multi-site 3PLs with a WMS they won't replace.
2. Customers burned by DockPilot's price increase.
3. Teams that already tried group chats and lost the history.

## Where we lose
- Single-site warehouses that just need dock scheduling (DockPilot).
- Very small teams that won't pay (Stackyard's free tier).

## Open questions
- Should we build a DockPilot import to make switching easier?
- Do we need SOC 2 before selling to 3PLs with retail clients?
