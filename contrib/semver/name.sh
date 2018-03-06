#!/bin/sh

# Get the branch name
BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null)

# Check if the branch name is not master
if [ "$BRANCH" = "master" ]; then
  printf "yggdrasil"
  exit 0
fi

# If it is something other than master, append it
# Remove any "/" characters from e.g. "pull/N"
printf "yggdrasil-%s" "${BRANCH/\//}"
