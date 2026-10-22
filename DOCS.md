Documentation uses [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/) and is hosted on GitHub Pages.

To build the docs locally, run:

```bash
make gendocs
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

Visit http://localhost:8000 to view the docs.

Updates to the `docs` directory will be automatically published to GitHub Pages on merge to `main`.


## Quick Start Bundling

To bundle the quick start guide into a single script, run:

```bash
make k8s-bundle
```

This will create an updated `bundle.sh` script in the `build/k8s` directory that can be run to deploy the quick start guide. The bundling process uses Docker for cross-platform compatibility.
