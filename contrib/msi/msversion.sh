#!/bin/sh

# Get the last tag
TAG=$(git describe --abbrev=0 --tags --match="v[0-9]*\.[0-9]*\.[0-9]*" 2>/dev/null)

# Did getting the tag succeed?
if [ $? != 0 ] || [ -z "$TAG" ]; then
  printf -- "unknown"
  exit 0
fi

# Get the current branch
BRANCH=$(git symbolic-ref -q HEAD --short 2>/dev/null)

# Did getting the branch succeed?
if [ $? != 0 ] || [ -z "$BRANCH" ]; then
  BRANCH="master"
fi

# Split out into major, minor and patch numbers
MAJOR=$(echo $TAG | cut -c 2- | cut -d "." -f 1)
MINOR=$(echo $TAG | cut -c 2- | cut -d "." -f 2)
PATCH=$(echo $TAG | cut -c 2- | cut -d "." -f 3 | awk -F"rc" '{print $1}')

# Output in the desired format
if [ $((PATCH)) -eq 0 ]; then
  printf '%s%d.%d' "$PREPEND" "$((MAJOR))" "$((MINOR))"
else
  printf '%s%d.%d.%d' "$PREPEND" "$((MAJOR))" "$((MINOR))" "$((PATCH))"
fi

# Add the build tag on non-master branches
if [ "$BRANCH" != "master" ]; then
  BUILD=$(git rev-list --count $TAG..HEAD 2>/dev/null)

  # Did getting the count of commits since the tag succeed?
  if [ $? != 0 ] || [ -z "$BUILD" ]; then
    printf -- "-unknown"
    exit 0
  fi

  # Is the build greater than zero?
  if [ $((BUILD)) -gt 0 ]; then
      printf -- "-%04d" "$((BUILD))"
  fi
fi