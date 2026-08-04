#!/bin/sh
set -eu

case "${1:-}" in
  validate)
    printf '%s\n' '[{"file":"api.http","ok":true}]'
    ;;
  --version)
    printf '%s\n' 'hitspec fixture'
    ;;
  *)
    exit 2
    ;;
esac
