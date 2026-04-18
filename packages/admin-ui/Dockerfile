# syntax=docker/dockerfile:1

FROM node:22-alpine AS deps
WORKDIR /src
COPY package.json ./
# Use npm (no lockfile shipped) — Next's standalone output doesn't care.
RUN npm install --no-audit --no-fund

FROM node:22-alpine AS build
WORKDIR /src
COPY --from=deps /src/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# next.config.mjs has output: 'standalone', so .next/standalone holds a
# self-contained server.js that only needs .next/static + public to run.
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3100
ENV HOSTNAME=0.0.0.0

COPY --from=build /src/.next/standalone ./
COPY --from=build /src/.next/static ./.next/static
# no public/ directory in this app — add this line back if one lands

USER node
EXPOSE 3100
CMD ["node", "server.js"]
