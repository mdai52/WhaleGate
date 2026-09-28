# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26
ARG NODE_VERSION=22
ARG ALPINE_VERSION=3.20

# ---------------- Frontend ----------------
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /web
COPY WhaleGate-frontend/package.json WhaleGate-frontend/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY WhaleGate-frontend/ ./
RUN npm run build

# ---------------- Backend ----------------
FROM golang:${GO_VERSION}-alpine AS server
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /src
COPY WhaleGate-backend/go.mod WhaleGate-backend/go.sum ./
RUN go mod download
COPY WhaleGate-backend/ .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/whalegate ./cmd/whalegate

# ---------------- Runtime ----------------
FROM alpine:${ALPINE_VERSION}
RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S whalegate && adduser -S -G whalegate whalegate
WORKDIR /app
COPY --from=server /out/whalegate /app/whalegate
COPY --from=web /web/dist /app/WhaleGate-frontend/dist
COPY --from=server /src/configs /app/configs
COPY --from=server /src/migrations /app/migrations
RUN chown -R whalegate:whalegate /app
USER whalegate
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/whalegate"]
CMD ["-c", "/app/configs/config.yaml"]
