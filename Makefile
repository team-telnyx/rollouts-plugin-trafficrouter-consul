# Build targets used by the Telnyx docker-image workflow
# (team-telnyx/infra-ci-gha-workflows dockerImage.yml): `make build` must leave
# $(red_image) in the local Docker image store and `make test` must exit
# non-zero on failure. Everything else is for local development.

registry  := registry.internal.telnyx.com
name      := $(shell awk '/^  service:/ {print $$2; exit}' meta-dev.yml)
namespace := $(shell awk '/^  namespace:/ {print $$2; exit}' meta-dev.yml)
tag       := red
image     := $(registry)/$(namespace)/$(name)
red_image := $(image):$(tag)

# Both architectures the argo-rollouts controller runs on. Loading a
# multi-platform build into the local store needs Docker's containerd image
# store (enabled on the CI runners). Override for a quick local build:
#   make build platforms=linux/arm64
platforms := linux/amd64,linux/arm64

git_commit := $(shell git rev-parse HEAD)
git_short  := $(shell git rev-parse HEAD | cut -c1-7)
version    := $(shell tr -d '[:space:]' < VERSION)
# Mirror the workflow's service version (<VERSION>.b<run>.<sha>) when running
# in GitHub Actions so the OCI version label matches the pushed tag.
ifdef GITHUB_RUN_NUMBER
image_version := $(version).b$(GITHUB_RUN_NUMBER).$(git_short)
else
image_version := $(version)
endif
source_url := $(if $(GITHUB_REPOSITORY),$(GITHUB_SERVER_URL)/$(GITHUB_REPOSITORY),https://github.com/team-telnyx/rollouts-plugin-trafficrouter-consul)
created    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# SBOM generator pulled through the internal proxy instead of Docker Hub.
sbom_generator := $(registry)/docker/docker/buildkit-syft-scanner:stable-1

build_args := \
	--build-arg VERSION=$(version) \
	--build-arg GIT_COMMIT=$(git_commit)

# The OCI labels are stamped here, at build time, so the docker-image workflow
# publishes the multi-platform index as-is instead of re-labelling it (which
# would flatten it to one platform and drop the attestations).
oci_labels := \
	--label org.opencontainers.image.created=$(created) \
	--label org.opencontainers.image.source=$(source_url) \
	--label org.opencontainers.image.revision=$(git_commit) \
	--label org.opencontainers.image.version=$(image_version)

.PHONY: build test lint fmt version push inspect help

build: ## Build the multi-arch image with provenance and SBOM attestations into the local store
	docker buildx build \
		--platform $(platforms) \
		--provenance=mode=max \
		--sbom=generator=$(sbom_generator) \
		$(build_args) $(oci_labels) \
		--target release \
		--load \
		-t $(red_image) .

test: ## Run go vet and go test inside the build image
	docker buildx build $(build_args) --target test .

lint: ## Run golangci-lint on the host
	golangci-lint run ./...

fmt: ## Fail if any file is not gofmt-clean
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

version: ## Print the image version the next build would be labelled with
	@echo $(image_version)

push: ## Push $(red_image) by hand. CI is the only sanctioned publisher; this is for debugging only.
	docker push $(red_image)

inspect: ## Show platforms and attestations of the pushed :red image
	docker buildx imagetools inspect $(red_image)

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
