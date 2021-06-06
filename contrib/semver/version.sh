#!/bin/sh

case "$*" in
  *--bare*)
    # Remove the "v" prefix
    git describe --tags --match="v[0-9]*\.[0-9]*\.[0-9]*" | cut -c 2-
    ;;
  *)
    git describe --tags --match="v[0-9]*\.[0-9]*\.[0-9]*"
    ;;
esac
