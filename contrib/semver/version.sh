#!/bin/sh

# Merge commits from this branch are counted
DEVELOPBRANCH="yggdrasil-network/develop"

# Get the last tag
TAG=$(git describe --abbrev=0 --tags --match="v[0-9]*\.[0-9]*\.0" 2>/dev/null)

# Get last merge to master
MERGE=$(git rev-list $TAG..master --grep "from $DEVELOPBRANCH" 2>/dev/null | head -n 1)

# Get the number of merges since the last merge to master
PATCH=$(git rev-list $TAG..master --count --merges --grep="from $DEVELOPBRANCH" 2>/dev/null)

# If it fails then there's no last tag - go from the first commit
if [ $? != 0 ]; then
  PATCH=$(git rev-list HEAD --count 2>/dev/null)

  # Complain if the git history is not available
  if [ $? != 0 ]; then
    printf 'unknown'
    exit -1
  fi

  printf 'v0.0.%d' "$PATCH"
  exit -1
fi

# Get the number of merges on the current branch since the last tag
BUILD=$(git rev-list $TAG..HEAD --count --merges)

# Split out into major, minor and patch numbers
MAJOR=$(echo $TAG | cut -c 2- | cut -d "." -f 1)
MINOR=$(echo $TAG | cut -c 2- | cut -d "." -f 2)

# Get the current checked out branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)

# Output in the desired format
if [ $PATCH = 0 ]; then
  if [ ! -z $FULL ]; then
    printf 'v%d.%d.0' "$MAJOR" "$MINOR"
  else
    printf 'v%d.%d' "$MAJOR" "$MINOR"
  fi
else
  printf 'v%d.%d.%d' "$MAJOR" "$MINOR" "$PATCH"
fi

# Add the build tag on non-master branches
if [ $BRANCH != "master" ]; then
  if [ $BUILD != 0 ]; then
    printf -- "-%04d" "$BUILD"
  fi
fi
