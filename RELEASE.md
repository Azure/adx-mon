# Release flow

## Publishing artifacts
The release process involves building and publishing container images for the following components of adx-mon:
- Alerter
- Collector
- Ingestor
- Operator

This part of the release process has been automated using the [Publish container images](https://github.com/Azure/adx-mon/actions/workflows/publish-container-images.yaml) GitHub action.
This action is triggered when either of these conditions are met:
- A new commit is pushed to the `main` branch
- A new tag is pushed to the repository

The trigger determines what tag will be used for the container images:
- `latest`: for when a new commit is pushed to the `main` branch
- `vX.Y.Z`: for when a new tag is pushed to the repository

> Note: For the `vX.Y.Z` tag, the version number is extracted from the tag name and used to tag the container images.
> Example - If the tag is `v1.0.0`, the collector image created will be `ghcr.io/azure/adx-mon/collector:v1.0.0`

## Creating the GitHub release
After pushing a `vX.Y.Z` tag, wait for the `Publish container images` workflow to complete successfully. Then create and publish a GitHub Release for the same tag with generated release notes:

```sh
gh release create vX.Y.Z \
  --repo Azure/adx-mon \
  --title "vX.Y.Z" \
  --generate-notes \
  --latest
```

GitHub generates the changelog from changes since the previous release and includes a full comparison link.

## Versioning
This project uses [Semantic Versioning](https://semver.org/).

Example: `v1.0.0`
