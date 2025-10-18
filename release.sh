#!/usr/bin/env bash
# ------------------------------------------------------------------
# Release script for the PiHole Operator.
#
# Usage: ./release.sh [--dry-run|-n] [major|minor|patch]
#
#   --dry-run / -n  : Show the changes that would be made, then exit.
#   major|minor|patch: Bump type (default is patch).
#
# What it does:
#   1. Finds the latest SemVer tag in the repo.
#   2. Bumps that version according to the supplied type.
#   3. Updates Chart.yaml (appVersion & chart version).
#   4. Commits the change, tags the repo and pushes everything.
#   5. Prints all commit messages since the previous tag (release notes).
# ------------------------------------------------------------------
set -euo pipefail

###########################
# 1. Configuration
###########################

CHART_FILE="dist/chart/Chart.yaml"
BUMP_TYPE=patch          # default, may be overridden later
DRY_RUN=0

###########################
# 2. Parse arguments
###########################

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run|-n)
            DRY_RUN=1
            shift
            ;;
        major|minor|patch)
            BUMP_TYPE="$1"
            shift
            ;;
        *)
            echo "❌ Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

###########################
# 3. Helper: bump a semver string
###########################

bump_semver() {
    local version=$1
    local type=$2

    IFS='.' read -r major minor patch <<< "$version"

    case "$type" in
        major) major=$((major + 1)); minor=0; patch=0 ;;
        minor) minor=$((minor + 1)); patch=0 ;;
        patch) patch=$((patch + 1)) ;;
        *) echo "❌ Unknown bump type: $type" >&2; exit 1 ;;
    esac

    echo "$major.$minor.$patch"
}

###########################
# 4. Detect current version
###########################

git fetch --tags

LATEST_TAG=$(git tag --list | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1)

if [[ -z "$LATEST_TAG" ]]; then
    echo "⚠️ No SemVer tags found – starting from 0.0.0"
    PREV_VERSION="0.0.0"
else
    PREV_VERSION="$LATEST_TAG"
fi

echo "🟢 Current repo version: $PREV_VERSION"

###########################
# 5. Compute new versions
###########################

NEW_APP_VERSION=$(bump_semver "$PREV_VERSION" "$BUMP_TYPE")
echo "🟢 New appVersion: $NEW_APP_VERSION"

CURRENT_CHART_VERSION=$(grep '^version:' "$CHART_FILE" | awk '{print $2}')
if [[ -z "$CURRENT_CHART_VERSION" ]]; then
    echo "❌ Cannot read chart version from $CHART_FILE"
    exit 1
fi

NEW_CHART_VERSION=$(bump_semver "$CURRENT_CHART_VERSION" "$BUMP_TYPE")
echo "🟢 New chart version: $NEW_CHART_VERSION"

###########################
# 6. Dry‑run mode
###########################

if [[ $DRY_RUN -eq 1 ]]; then
    echo ""
    echo "🚨 Dry‑run: no changes will be written."
    echo "Would change appVersion from $PREV_VERSION to $NEW_APP_VERSION"
    echo "Would change chart version from $CURRENT_CHART_VERSION to $NEW_CHART_VERSION"
    echo ""
    echo "Would run:"
    echo "  sed -i.bak \"s/^appVersion:.*/appVersion: $NEW_APP_VERSION/\" $CHART_FILE"
    echo "  sed -i.bak \"s/^version:.*/version: $NEW_CHART_VERSION/\"   $CHART_FILE"
    echo ""
    echo "Would commit: Release $NEW_APP_VERSION (Chart: $NEW_CHART_VERSION)"
    echo "Would tag: $NEW_APP_VERSION"
    echo "Would push commit and tags."
    exit 0
fi

###########################
# 7. Update Chart.yaml
###########################

sed -i.bak "s/^appVersion:.*/appVersion: $NEW_APP_VERSION/" "$CHART_FILE"
sed -i.bak "s/^version:.*/version: $NEW_CHART_VERSION/"   "$CHART_FILE"
rm -f "${CHART_FILE}.bak"

###########################
# 8. Commit & tag
###########################

git add "$CHART_FILE"
git commit -m "Release $NEW_APP_VERSION (Chart: $NEW_CHART_VERSION)"
git tag "$NEW_APP_VERSION"

###########################
# 9. Push everything
###########################

git push
git push --tags

###########################
# 10. Release notes
###########################

echo ""
echo "📝 Release notes (commits since $PREV_VERSION):"
# If PREV_VERSION is 0.0.0 (no tag), show all commits
if [[ "$PREV_VERSION" == "0.0.0" ]]; then
    git log --pretty=format:"- %h %s"
else
    git log --pretty=format:"- %h %s" "$PREV_VERSION"..HEAD
fi

echo ""
echo "✅ Released $NEW_APP_VERSION (Chart: $NEW_CHART_VERSION) – all pushed!"
