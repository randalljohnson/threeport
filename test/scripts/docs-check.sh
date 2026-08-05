#!/usr/bin/env bash

# Validate the documentation site: every internal link resolves to a page that
# exists, every anchor a link points at is present, and no page is stranded
# outside the navigation. Run from the repository root.

set -euo pipefail

# mkdocs and the material theme are not part of the Go toolchain, so check for
# them up front rather than letting the build fail with a config error.
if ! command -v mkdocs > /dev/null 2>&1; then
    echo "docs check failed:"
    echo
    echo "mkdocs was not found on PATH"
    echo "install the documentation toolchain with:"
    echo
    echo "  pip install -r docs/requirements.txt"

    exit 1
fi

# Render into a throwaway directory; only the warnings matter here, and the
# published site is built separately at release time.
SITE_DIR=`mktemp -d`
trap 'rm -rf "${SITE_DIR}"' EXIT

# Strict mode turns every warning into a failure. Which problems count as
# warnings is set by the validation block in docs/mkdocs.yml.
cd docs
mkdocs build --strict --site-dir "${SITE_DIR}"
