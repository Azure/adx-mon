Documentation uses [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/) and is hosted on GitHub Pages.

To build the docs locally, run:

```bash
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

Visit http://localhost:8000 to view the docs.

Updates to the `docs` directory will be automatically published to GitHub Pages on merge to `main`.
