#!/bin/sh

# Get the branch name, removing any "/" characters from pull requests
BRANCH=$(git symbolic-ref --short HEAD | tr -d "/" 2>/dev/null)

# Check if the branch name is not master
if [ "$BRANCH" = "master" ]; then
  printf "yggdrasil"
  exit 0
fi

# If it is something other than master, append it
printf "yggdrasil-%s" "$BRANCH"
