Documentation uses [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/) and is hosted on GitHub Pages.

To build the docs locally, run:

```bash
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

Visit http://localhost:8000 to view the docs.

Updates to the `docs` directory will be automatically published to GitHub Pages on merge to `main`.


## Quick Start Bundling

To bundle the quick start guide into a single script, run:

```bash
$ docker run -it -v $(pwd)/build/k8s:/build ubuntu bash 
$ apt update && apt install -y sharutils && cd /build && rm -f ./bundle.sh && shar -M . > /tmp/bundle.sh && cp /tmp/bundle.sh .
```

This will create a `bundle.sh` script in the `build/k8s` directory that can be run to deploy the quick start guide.
