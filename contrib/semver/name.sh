#!/bin/sh

# Get the branch name
BRANCH=$(git name-rev --name-only HEAD 2>/dev/null)

# Check if the branch name is not master
if [ "$BRANCH" = "master" ]; then
  printf "yggdrasil"
  exit 0
fi

# If it is something other than master, append it
printf "yggdrasil-%s" "$BRANCH"
