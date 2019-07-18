#!/bin/sh

# Get the last tag
TAG=$(git describe --abbrev=0 --tags --match="v[0-9]*\.[0-9]*\.[0-9]*" 2>/dev/null)

# Did getting the tag succeed?
if [ $? != 0 ] || [ -z "$TAG" ]; then
  printf -- "unknown"
  exit 1
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
PATCH=$(echo $TAG | cut -c 2- | cut -d "." -f 3)

main () {
    # Check if the branch name is master
    if [ "$BRANCH" = "master" ]; then
        printf "latest"
        exit 0
    fi
    # If it is something other than master, just use it
    printf "$BRANCH"
}

full () {
    if [ "$BRANCH" = "master" ]; then
        printf '%d.%d.%d' "$((MAJOR))" "$((MINOR))" "$((PATCH))"
        exit 0
    fi
    printf '%d.%d.%d-%s' "$((MAJOR))" "$((MINOR))" "$((PATCH))" "$BRANCH"
}

majorminor () {
    if [ "$BRANCH" = "master" ]; then
        printf '%d.%d' "$((MAJOR))" "$((MINOR))"
        exit 0
    fi
    printf '%d.%d-%s' "$((MAJOR))" "$((MINOR))" "$BRANCH"
}

major () {
    if [ "$BRANCH" = "master" ]; then
        printf "$((MAJOR))"
        exit 0
    fi
    printf '%d-%s' "$((MAJOR))" "$BRANCH"
}

for arg in "$@"; do
    $arg
done
