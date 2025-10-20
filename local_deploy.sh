#!/usr/bin/env bash
set -euo pipefail

# File to keep track of the version
VERSION_FILE=".local_version"

# Read current version or default to 0.0.0
if [[ -f "$VERSION_FILE" ]]; then
  VERSION=$(cat "$VERSION_FILE")
else
  VERSION="0.0.0"
fi

# Split version into parts
IFS='.' read -r major minor patch <<< "$VERSION"

# Determine bump type (default: patch)
BUMP_TYPE=${1:-patch}

case "$BUMP_TYPE" in
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  patch)
    patch=$((patch + 1))
    ;;
  *)
    echo "❌ Unknown bump type: $BUMP_TYPE"
    echo "Usage: $0 [major|minor|patch]"
    exit 1
    ;;
esac

# New version
NEW_VERSION="$major.$minor.$patch"

# Save new version
echo "$NEW_VERSION" > "$VERSION_FILE"

# Image name
IMG="registry.supporterino.de/supporterino/pihole-operator:$NEW_VERSION"

# Run make commands
make docker-build IMG="$IMG"
make docker-push IMG="$IMG"
make deploy IMG="$IMG"

echo "✅ Deployed version $NEW_VERSION"
