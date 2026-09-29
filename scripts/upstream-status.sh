#!/usr/bin/env bash
# Report how this fork relates to charmbracelet/soft-serve before a sync.
# See FORK.md for the full sync procedure.
set -euo pipefail

UPSTREAM_URL=https://github.com/charmbracelet/soft-serve.git
UPSTREAM_REF=${UPSTREAM_REF:-upstream/main}

if ! git remote get-url upstream >/dev/null 2>&1; then
	git remote add upstream "$UPSTREAM_URL"
fi
git fetch --quiet upstream

base=$(git merge-base HEAD "$UPSTREAM_REF")
echo "Merge base:      $(git log -1 --format='%h %s' "$base")"
echo "Fork commits:    $(git rev-list --count "$base"..HEAD)"
echo "Upstream ahead:  $(git rev-list --count "$base".."$UPSTREAM_REF")"
echo

# Upstream migrations share one integer sequence; fork migrations live on
# their own (pkg/db/migrate/fork.go). New upstream ones need no renumbering,
# but check they don't touch fork tables.
new_migrations=$(git diff --name-only --diff-filter=A "$base" "$UPSTREAM_REF" -- 'pkg/db/migrate/*' || true)
if [ -n "$new_migrations" ]; then
	echo "New upstream migrations (check they don't collide with fork tables):"
	echo "$new_migrations" | sed 's/^/  /'
	echo
fi

# Upstream-owned files the fork modifies (working tree, so uncommitted
# changes count too). Every entry is a potential
# conflict; keep each change to a one-line hook marked "// fork".
echo "Upstream files modified by the fork:"
git diff --numstat --diff-filter=MD "$UPSTREAM_REF" |
	awk '{ printf "  +%-4s -%-4s %s\n", $1, $2, $3 }'
echo

overlap=$(comm -12 \
	<(git diff --name-only "$base" HEAD | sort) \
	<(git diff --name-only "$base" "$UPSTREAM_REF" | sort))
if [ -n "$overlap" ]; then
	echo "Changed on both sides since the merge base (expect conflicts):"
	echo "$overlap" | sed 's/^/  /'
fi
