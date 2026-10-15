# Product Roadmap — Q4 2026

Owner: Leah Brandt. Updated 2026-10-06 after the planning offsite.

## Committed
1. **Exception Inbox GA** — target launch 2026-11-12.
2. **Shift Handoff Notes v2** — acknowledgements and handoff templates per site.
3. **Northgate WMS integration** — read-only sync of orders and locations.
4. SSO (Google and Okta) — required by two pilots before they renew.

## Cut from this quarter
- **Mobile scanner app** — cut. Pilots use rugged Android devices with their
  own scanner apps; we'll support those through a web view instead.
- **Spanish localization** — pushed to Q1 2027. Bluewater asked for it, but no
  pilot has made it a renewal condition.
- DockPilot import tool — deferred until we see more switching deals.

## Launch risks (Exception Inbox GA)
- SSO might slip past Nov 12; two pilots won't roll out more widely without it.
- Label printer integration has open bugs at the Pine Ridge site.
- Only one engineer (Marcus) knows the WMS sync code, so we depend on one person.
- Support load: no on-call rotation yet for weekend shifts.

## Metrics we're watching
- Weekly active supervisors per site.
- Exceptions resolved within one shift.
- Handoff notes acknowledged by the next shift.
