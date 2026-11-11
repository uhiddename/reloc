# Base images are pinned by digest: a tag can later point to another build,
# a digest can't. The tag stays for people; to update, bump both together.

# Build stage: runs natively on the build machine, even for another target.
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:e519d3b0a5b285e5aa53ab9eef02bc6bbb9cb04608389b743a136a9ce75f0d1e AS build
WORKDIR /reloc/src

# Dependencies come first, so code changes don't download them again.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Set by BuildKit from the requested --platform.
ARG TARGETOS
ARG TARGETARCH
# Set by CI (--build-arg=VERSION=...). The image has no .git for Go to read
# the version from; left empty, the binaries report "dev".
ARG VERSION
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
             -ldflags="-s -w -X=github.com/uhiddename/reloc/internal/buildinfo.version=${VERSION}" \
             -o=/reloc/dist/ ./cmd/reloc ./cmd/relocd

# Base of the final stages: CA certificates and a non-root user, no shell.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS runtime

# Final stages, one image per binary. Both copy from the same build stage,
# so Go compiles once.

# The CLI: docker build --target reloc -t reloc:dev .
FROM runtime AS reloc
COPY --from=build /reloc/dist/reloc /usr/local/bin/
ENTRYPOINT ["reloc"]

# The server, last so that a build without --target gives it:
# docker build --target relocd -t relocd:dev .
FROM runtime AS relocd
COPY --from=build /reloc/dist/relocd /usr/local/bin/

# Documentation only: publishing the port is up to whoever runs the image.
EXPOSE 8080

# Exec form: relocd runs as PID 1 and receives SIGTERM directly.
ENTRYPOINT ["relocd"]

# Another architecture: add --platform linux/amd64.
