FROM node:22-slim AS builder
WORKDIR /app

RUN npm install -g pnpm@10.21.0

COPY . .
RUN pnpm install --frozen-lockfile
RUN pnpm --filter docs run build

FROM node:22-slim
WORKDIR /app
RUN npm install -g serve
COPY --from=builder /app/typescript/apps/docs/dist ./dist
EXPOSE 3000
CMD ["serve", "dist", "-s", "-l", "3000"]
