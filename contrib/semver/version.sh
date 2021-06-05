#!/bin/sh

# We'll just use the `git describe` version since it's reasonably
# easy to refer to a tag or commit using the describe output.
git describe --match="v[0-9]*\.[0-9]*\.[0-9]*" 2>/dev/null
