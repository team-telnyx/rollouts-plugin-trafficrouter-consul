## 0.0.3 (unreleased)

* Image is built and published by CI (Telnyx docker-image workflow) as a
  linux/amd64,linux/arm64 index with provenance and SBOM attestations, to
  registry.internal.telnyx.com/infra/rollouts-plugin-trafficrouter-consul.
  Replaces the hand-built 0.0.2-header-routes image. [CI-351]
* HashiCorp CRT release tooling (build.yml, build-support, .release) removed
  from the fork; the Dockerfile is now a self-contained multi-stage build.

## 0.0.2 (unreleased, hand-built as 0.0.2-header-routes)

* Implement SetHeaderRoute and RemoveManagedRoutes. [CI-305]
* Quote the Service.Meta version value in the ServiceResolver subset filter. [INFRA-427]

## 0.0.1 (Jun 24, 2024)

* Initial releasee

## 0.0.1-alpha1 (March 6, 2024)

* Initial alpha release
