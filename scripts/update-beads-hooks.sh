#!/usr/bin/env bash
# update-beads-hooks.sh — refresh bd hooks in every repo with a .beads/ dir.
#
# Walks ~/.* and ~/code/* recursively, finds every .beads/ directory, and runs
# `bd hooks install --beads --force` in its enclosing git repo. Useful after
# upgrading the bd binary so per-repo shims pick up new bd-shim format and any
# repo with missing/stale hooks gets brought back into shape.
#
# The fix itself ships in the bd binary (the .beads/hooks/* shims just call
# `bd hooks run …`), so simply running this is not what makes a fix take
# effect — the binary upgrade does. This script exists to surface stale shims
# and re-assert hook presence across the machine.
#
# Skips:
#   - directories that aren't inside a git repo
#   - worktrees (they share core.hooksPath with the main repo, so updating
#     the main repo covers them)
#
# Usage:
#   scripts/update-beads-hooks.sh            # do it
#   scripts/update-beads-hooks.sh --dry-run  # show what would be done
#   scripts/update-beads-hooks.sh --help

set -euo pipefail

DRY_RUN=false
case "${1:-}" in
    -n|--dry-run) DRY_RUN=true ;;
    -h|--help)
        sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
        exit 0
        ;;
    "") ;;
    *)
        printf 'unknown flag: %s\n' "$1" >&2
        exit 2
        ;;
esac

if [[ -t 1 ]]; then
    RED=$'\e[31m' GREEN=$'\e[32m' YELLOW=$'\e[33m' DIM=$'\e[2m' BOLD=$'\e[1m' NC=$'\e[0m'
else
    RED='' GREEN='' YELLOW='' DIM='' BOLD='' NC=''
fi

if ! command -v bd >/dev/null 2>&1; then
    printf '%serror:%s bd not on PATH\n' "$RED" "$NC" >&2
    exit 1
fi

bd_version=$(bd --version 2>/dev/null | head -1 || true)
printf '%sUsing %s%s\n\n' "$BOLD" "${bd_version:-unknown bd}" "$NC"

shopt -s nullglob
roots=()
for d in "$HOME"/.[!.]*/ "$HOME"/code/*/; do
    [[ -d "$d" ]] && roots+=("${d%/}")
done

if [[ ${#roots[@]} -eq 0 ]]; then
    printf '%sno candidate roots under ~/.* or ~/code/*%s\n' "$YELLOW" "$NC" >&2
    exit 0
fi

# Find every .beads/ directory under the roots, pruning trees we never want
# to descend into. The prune list is biased toward large, dependency-managed
# trees (node_modules, vendor, target, .cache, etc.).
candidates=()
while IFS= read -r -d '' beads; do
    candidates+=("$beads")
done < <(
    find "${roots[@]}" \
        \( -name .git -o -name node_modules -o -name vendor -o -name target \
           -o -name dist -o -name build -o -name out \
           -o -name Library -o -name Caches -o -name .cache \
           -o -name .npm -o -name .gem -o -name .cargo -o -name .rustup \
           -o -name __pycache__ -o -name .pytest_cache -o -name .mypy_cache \
           -o -name .tox -o -name .terraform -o -name .gradle \
           -o -name .next -o -name .nuxt \) \
        -prune -o \
        -type d -name .beads -print0 \
        2>/dev/null
)

printf 'Found %s%d%s .beads candidate(s) under ~/.* and ~/code/*\n\n' \
    "$BOLD" "${#candidates[@]}" "$NC"

# Track repos we've already updated so a repo with multiple .beads dirs (rare)
# doesn't get updated twice.
declare -a seen_repos=()
already_seen() {
    local needle=$1
    local item
    for item in "${seen_repos[@]}"; do
        [[ "$item" == "$needle" ]] && return 0
    done
    return 1
}

ok=0
skipped=0
failed=0
fails=()

for beads in "${candidates[@]}"; do
    repo=$(cd "$(dirname "$beads")" 2>/dev/null && pwd) || continue
    label=${repo/#$HOME/~}

    if already_seen "$repo"; then
        continue
    fi
    seen_repos+=("$repo")

    if ! git_dir=$(git -C "$repo" rev-parse --absolute-git-dir 2>/dev/null); then
        printf '  %sskip%s %-60s %s(not a git repo)%s\n' \
            "$DIM" "$NC" "$label" "$DIM" "$NC"
        skipped=$((skipped + 1))
        continue
    fi

    common=$(git -C "$repo" rev-parse --git-common-dir 2>/dev/null || true)
    common_abs=""
    if [[ -n "$common" ]]; then
        if [[ "$common" = /* ]]; then
            common_abs=$(cd "$common" 2>/dev/null && pwd || true)
        else
            common_abs=$(cd "$repo" && cd "$common" 2>/dev/null && pwd || true)
        fi
    fi
    if [[ -n "$common_abs" && "$git_dir" != "$common_abs" ]]; then
        printf '  %sskip%s %-60s %s(worktree)%s\n' \
            "$DIM" "$NC" "$label" "$DIM" "$NC"
        skipped=$((skipped + 1))
        continue
    fi

    if $DRY_RUN; then
        printf '  %swould update%s %s\n' "$YELLOW" "$NC" "$label"
        ok=$((ok + 1))
        continue
    fi

    if output=$(cd "$repo" && bd hooks install --beads --force 2>&1 </dev/null); then
        printf '  %sok%s   %s\n' "$GREEN" "$NC" "$label"
        ok=$((ok + 1))
    else
        printf '  %sfail%s %s\n' "$RED" "$NC" "$label"
        fails+=("$label"$'\n'"$output")
        failed=$((failed + 1))
    fi
done

printf '\n%sSummary:%s ok=%d skipped=%d failed=%d\n' \
    "$BOLD" "$NC" "$ok" "$skipped" "$failed"

if [[ "$failed" -gt 0 ]]; then
    printf '\n%sFailures:%s\n' "$RED" "$NC"
    for f in "${fails[@]}"; do
        printf -- '----\n%s\n' "$f"
    done
    exit 1
fi
