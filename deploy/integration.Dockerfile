# Integration test runner: backend source is COPYed in at build time instead of
# bind-mounted. Jenkins runs its k8s agents over the host node's docker socket,
# where the Pod's workspace path does not exist — a bind mount there silently
# resolves to an empty host directory and `go test ./...` dies with "directory
# prefix . does not contain main module". Build contexts travel the socket as
# tar streams, so baked-in source works everywhere (same reason e2e passes).
FROM golang:1.25-alpine

WORKDIR /src/backend

# Layer order matches api.Dockerfile's gobuild stage: source edits above do not
# invalidate the module-download layer, and the node's docker layer cache keeps
# it warm across CI builds (each build gets a fresh compose project + volumes).
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./

# TEST_DATABASE_URL is provided by compose.test.yaml. The compose project
# mounts a named go_build volume for in-run compile caching; nothing is
# mounted over /go/pkg/mod on purpose: modules live in this image layer (a
# failed download fails the build, so the layer is always complete), while
# a named volume there is filled by copy-on-create, which a flaky CI node can
# interrupt into a truncated module cache that go then trusts blindly.
CMD ["go", "test", "./...", "-count=1"]