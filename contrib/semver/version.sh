#!/bin/sh

# Get the last tag
TAG=$(git describe --abbrev=0 --tags 2>/dev/null)

# Get the number of commits from the last tag, or if not, from
# the first commit
COUNT=$( \
  git rev-list v$TAG..HEAD --count 2>/dev/null || \
  git rev-list HEAD --count 2>/dev/null \
)

# Check if it matches the vX.Y.Z format
grep "v[0-9]*\.[0-9]*\.[0-9]*" <<< $TAG &>/dev/null

# If it doesn't, abort
if [ $? -ne 0 ]; then
  printf 'v0.0.0-%d' "$COUNT"
  exit -1
fi

# Trim the "v" off the front
TAG=$(echo $TAG | cut -c 2-)

# Split out into major, minor and patch numbers
MAJOR=$(echo $TAG | cut -d "." -f 1)
MINOR=$(echo $TAG | cut -d "." -f 2)
PATCH=$(echo $TAG | cut -d "." -f 3)

# Output in the desired format
printf 'v%d.%d.%d-%d' "$MAJOR" "$MINOR" "$PATCH" "$COUNT"
