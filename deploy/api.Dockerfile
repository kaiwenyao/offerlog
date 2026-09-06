# Multi-stage: builds the frontend, then the api+worker binaries, and serves
# the SPA from the final stage (Caddy proxies to the api container). The SPA
# stays embedded for the compose single-entry deployment; the split k8s
# deployment serves the SPA from the separate web image instead
# (deploy/web.Dockerfile).
FROM node:22-alpine AS fe
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS gobuild
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api-admin ./cmd/admin

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=gobuild /out/api /app/api
COPY --from=gobuild /out/worker /app/worker
COPY --from=gobuild /out/api-admin /app/api-admin
COPY --from=fe /fe/dist /srv
# The api binary embeds migrations; static assets live in /srv and are served
# via the "dist" candidate path resolution in cmd/api/main.go (parent of exe).
EXPOSE 8080
CMD ["/app/api"]
