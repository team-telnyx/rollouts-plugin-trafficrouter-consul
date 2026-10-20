# syntax=docker/dockerfile:1

# Build of the Consul traffic-router plugin for Argo Rollouts, as published by
# the Telnyx docker-image workflow (see .github/workflows/dockerImage.yml).
#
# The plugin is a static Go binary. The build stage always runs on the build
# platform and cross-compiles with GOOS/GOARCH, and the release stage contains
# no RUN instructions, so a linux/amd64,linux/arm64 image builds without QEMU.
#
# Images are pulled through the internal registry proxy rather than Docker Hub.

ARG GOLANG_IMAGE=registry.internal.telnyx.com/docker/library/golang:1.25-alpine
ARG BASE_IMAGE=registry.internal.telnyx.com/docker/library/alpine:3.22

# ---------------------------------------------------------------------------
# build: compile the plugin for TARGETOS/TARGETARCH
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${GOLANG_IMAGE} AS build

# git is needed so `go build` can stamp vcs.revision / vcs.modified into the
# binary's build info; that is what reviewers inspect to tie a binary to a
# commit, independently of the image provenance attestation.
RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
# VERSION is the release version (contents of ./VERSION); GIT_COMMIT the full
# commit sha. Both are set by the Makefile.
ARG VERSION=dev
ARG GIT_COMMIT=unknown

ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w \
        -X github.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/pkg/version.Version=${VERSION} \
        -X github.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/pkg/version.VersionPrerelease= \
        -X github.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/pkg/version.GitCommit=${GIT_COMMIT}" \
      -o /out/rollouts-plugin-trafficrouter-consul .

# ---------------------------------------------------------------------------
# test: unit tests and vet, run natively on the build platform
# (`make test`, and the "Test Image" stage of the docker-image workflow)
# ---------------------------------------------------------------------------
FROM build AS test
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go vet ./... && go test ./...

# ---------------------------------------------------------------------------
# release: the published image
# ---------------------------------------------------------------------------
FROM ${BASE_IMAGE} AS release

ARG VERSION=dev

LABEL org.opencontainers.image.title="rollouts-plugin-trafficrouter-consul" \
      org.opencontainers.image.description="Argo Rollouts traffic-router plugin for Consul service mesh (Telnyx build)" \
      org.opencontainers.image.vendor="Telnyx" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"

COPY LICENSE /usr/share/doc/rollouts-plugin-trafficrouter-consul/LICENSE.txt
COPY --from=build /out/rollouts-plugin-trafficrouter-consul /bin/rollouts-plugin-trafficrouter-consul

# The argo-rollouts controller runs this image as an init container that copies
# /bin/rollouts-plugin-trafficrouter-consul into a shared emptyDir, so the
# image keeps alpine's /bin/sh and cp. Same non-root uid as the upstream image.
USER 100:100
CMD ["/bin/rollouts-plugin-trafficrouter-consul"]
