#!/bin/sh

# Get the branch name, removing any "/" characters from pull requests
BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null | tr -d "/")

# Check if the branch name is not master
if [ "$BRANCH" = "master" ] || [ $? != 0 ]; then
  printf "yggdrasil"
  exit 0
fi

# If it is something other than master, append it
printf "yggdrasil-%s" "$BRANCH"
