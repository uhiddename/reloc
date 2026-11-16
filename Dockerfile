# Base images are pinned by digest: a tag can later point to another build,
# a digest can't. The tag stays for people; to update, bump both together.

# UI stage: the output is plain JS and CSS, the same for every target, so
# it always runs natively. Node matches the version used locally (fnm).
FROM --platform=$BUILDPLATFORM node:26.13.1-trixie-slim@sha256:e6bedef217677b276c591795e169840edd9096dbf13f29df8ef8d1856b8820bc AS web
WORKDIR /reloc/web

# Dependencies first, as in the Go stage. npm ci installs exactly the lock
# file and fails if it disagrees with package.json; --ignore-scripts keeps
# install scripts of dependencies from running, the build needs none.
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts

COPY web/ ./
RUN npm run build

# Build stage: runs natively on the build machine, even for another target.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS build
WORKDIR /reloc/src

# Dependencies come first, so code changes don't download them again.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Over the empty web/dist from the context, before go build embeds it.
COPY --from=web /reloc/web/dist/ web/dist/

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
