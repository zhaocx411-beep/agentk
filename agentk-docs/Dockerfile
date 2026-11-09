FROM registry.corp.kuaishou.com/ksbase/build/nodejs/node:24.19.0-bookworm AS builder

ARG PNPM_VERSION=11.24.0
ARG AGENTK_DOCS_PUBLIC_URL
ARG NPM_REGISTRY=https://npm.corp.kuaishou.com/

ENV npm_config_registry=${NPM_REGISTRY} \
    pnpm_config_registry=${NPM_REGISTRY}

WORKDIR /app

RUN npm install --global "pnpm@${PNPM_VERSION}"

COPY .npmrc package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY server/package.json server/package.json
RUN test "$(pnpm config get registry)" = "${NPM_REGISTRY}"
RUN pnpm install --frozen-lockfile

COPY . .
RUN AGENTK_DOCS_PUBLIC_URL="${AGENTK_DOCS_PUBLIC_URL}" node -e \
    "const u=new URL(process.env.AGENTK_DOCS_PUBLIC_URL);if(u.protocol!=='https:'||u.pathname!=='/docs/'||u.search||u.hash||u.username||u.password)process.exit(1)"
RUN AGENTK_DOCS_PUBLIC_URL="${AGENTK_DOCS_PUBLIC_URL}" pnpm build
RUN pnpm --filter @agentk/docs-server deploy --prod /runtime/server

FROM registry.corp.kuaishou.com/ksbase/runtime/nodejs/kcsize:24.19.0-kcsize1.0.0-bookworm-slim-r1

WORKDIR /app

ENV NODE_ENV=production \
    SUPERVISOR_PROGRAM0=agentk-docs \
    SUPERVISOR_COMMAND0="/usr/local/bin/node /app/server/dist/service.js"

COPY --from=builder /app/build /app/build
COPY --from=builder /app/generated/search-documents.json /app/generated/search-documents.json
COPY --from=builder /app/server/dist /app/server/dist
COPY --from=builder /runtime/server/package.json /app/server/package.json
COPY --from=builder /runtime/server/node_modules /app/server/node_modules
