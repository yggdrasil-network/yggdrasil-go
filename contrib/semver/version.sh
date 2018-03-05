#!/bin/sh

# Get the last tag
TAG=$(git describe --abbrev=0 --tags --match="v[0-9]*\.[0-9]*" 2>/dev/null)

# Get the number of commits from the last tag
COUNT=$(git rev-list $TAG..HEAD --count 2>/dev/null)

# If it fails then there's no last tag - go from the first commit
if [ $? != 0 ]; then
  COUNT=$(git rev-list HEAD --count 2>/dev/null)

  printf 'v0.0.%d' "$COUNT"
  exit -1
fi

# Split out into major, minor and patch numbers
MAJOR=$(echo $TAG | cut -c 2- | cut -d "." -f 1)
MINOR=$(echo $TAG | cut -c 2- | cut -d "." -f 2)

# Output in the desired format
if [ $COUNT = 0 ]; then
  printf 'v%d.%d' "$MAJOR" "$MINOR"
else
  printf 'v%d.%d.%d' "$MAJOR" "$MINOR" "$COUNT"
fi
