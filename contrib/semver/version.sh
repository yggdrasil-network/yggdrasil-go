#!/bin/sh

if [ "$1" == "--bare" ]; then
    # Remove the "v" prefix
    git describe --tags --match="v[0-9]*\.[0-9]*\.[0-9]*" | cut -c 2-
else
    git describe --tags --match="v[0-9]*\.[0-9]*\.[0-9]*"
fi
