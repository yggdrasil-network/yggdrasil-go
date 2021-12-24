#!/bin/sh

# Get the current branch name
BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null)

if [ -n "$APPVEYOR_PULL_REQUEST_NUMBER" ]; then
  printf "yggdrasil-pr%s" "$APPVEYOR_PULL_REQUEST_NUMBER"
  exit 0
# Complain if the git history is not available
elif [ $? != 0 ] || [ -z "$BRANCH" ]; then
  printf "yggdrasil"
  exit 0
fi

# Remove "/" characters from the branch name if present
BRANCH=$(echo $BRANCH | tr -d "/")

# Check if the branch name is not master
if [ "$BRANCH" = "master" ]; then
  printf "yggdrasil"
  exit 0
fi

# If it is something other than master, append it
printf "yggdrasil-%s" "$BRANCH"
