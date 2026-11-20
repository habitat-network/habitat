# Dev image for the Vite/TanStack apps. The workspace is bind-mounted at
# /workspace; deps install into Compose's node_modules volume on each start.
FROM node:26-bookworm-slim

# Node 26 no longer ships corepack, so install the pnpm version pinned in
# .prototools directly.
RUN npm install -g pnpm@11.28.12

WORKDIR /workspace

# Workers read their dev env from .dev.vars; Compose supplies hardcoded values
# as DEV_VAR_* so no per-app setup is needed. Then install from the mounted
# lockfile and run the service command Compose passes (e.g. `pnpm dev`) from
# the service's working_dir. The trailing `sh` is $0 for the shell, so "$@"
# picks up only the command args.
ENTRYPOINT ["/bin/sh", "-c", "vars=\"$(env | sed -n 's/^DEV_VAR_//p')\"; [ -z \"$vars\" ] || printf '%s\\n' \"$vars\" > .dev.vars; pnpm --dir /workspace install --frozen-lockfile --prefer-offline --store-dir /pnpm/store && exec \"$@\"", "sh"]
