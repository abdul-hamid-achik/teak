#!/bin/sh

# Keep terminal verification reproducible across local, CI, and release runs.
# Update this single pin deliberately when the spec contract is revalidated
# against a newer Glyphrun release.
set -eu

glyphrun_version=v0.16.1
exec go install "github.com/abdul-hamid-achik/glyphrun/cmd/glyph@${glyphrun_version}"
