#!/bin/sh

# Get the current branch name
BRANCH="$GITHUB_REF_NAME"
if [ -z "$BRANCH" ]; then
  BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null)
fi

# Complain if the git history is not available
if [ $? != 0 ] || [ -z "$BRANCH" ]; then
  printf "mesh"
  exit 0
fi

# Remove "/" characters from the branch name if present
BRANCH=$(echo $BRANCH | tr -d "/")

# Check if the branch name is not master
if [ "$BRANCH" = "master" ]; then
  printf "mesh"
  exit 0
fi

# If it is something other than master, append it
printf "mesh-%s" "$BRANCH"
