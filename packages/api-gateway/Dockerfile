# Multi-stage build for the Dominion api-gateway.
#
# Stage 1: build a static Linux binary on golang:1.25.
# Stage 2: ship it in a distroless image as a non-root user.

FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache module downloads first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags='-s -w' \
      -o /out/gateway .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/gateway /gateway

EXPOSE 3000
USER nonroot:nonroot

ENTRYPOINT ["/gateway"]
