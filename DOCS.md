Documentation uses [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/) and is hosted on GitHub Pages.

To build the docs locally, run:

```bash
go run tools/docgen/config/config.go
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

Visit http://localhost:8000 to view the docs.

Updates to the `docs` directory will be automatically published to GitHub Pages on merge to `main`.


## Quick Start Bundling

To bundle the quick start guide into a single script, run:

```bash
$ docker run -it -v $(pwd)/build/k8s:/build ubuntu bash 
$ cd /build && rm -f ./bundle.sh && tar czf /tmp/bundle.tar.gz . && base64 /tmp/bundle.tar.gz > /tmp/bundle.tar.gz.b64 && echo '#!/bin/bash' > bundle.sh && echo 'base64 -d << "EOF" | tar xz' >> bundle.sh && cat /tmp/bundle.tar.gz.b64 >> bundle.sh && echo 'EOF' >> bundle.sh && echo "./setup.sh" >> bundle.sh
```

This will create a `bundle.sh` script in the `build/k8s` directory that can be run to deploy the quick start guide.
