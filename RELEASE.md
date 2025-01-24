# Release flow

## Publishing artifacts
The release process involves building and publishing container images for the following components of adx-mon:
- Alerter
- Collector
- Ingestor

This part of the release process has been automated using the [Publish container images](https://github.com/Azure/adx-mon/actions/workflows/publish-container-images.yaml) GitHub action.
This action is triggered when either of these conditions are met:
- A new commit is pushed to the `main` branch
- A new tag is pushed to the repository

The trigger determines what tag will be used for the container images:
- `latest`: for when a new commit is pushed to the `main` branch
- `vX.Y.Z`: for when a new tag is pushed to the repository

> Note: For the `vX.Y.Z` tag, the version number is extracted from the tag name and used to tag the container images.
> Example - If the tag is `v1.0.0`, the collector image created will be `ghcr.io/azure/adx-mon/collector:v1.0.0`

## Versioning
This project uses [Semantic Versioning](https://semver.org/).

Example: `v1.0.0`
