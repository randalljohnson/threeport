#!/usr/bin/env bash

# Check that every command the documentation tells a reader to run actually
# exists. Only fenced code blocks are read, so prose that happens to mention a
# command name is ignored. Run from the repository root.
#
# The command line tool and the build system are asked for their own commands
# rather than having them listed here, so a rename is caught the moment the
# tool changes and this script never carries a second copy of the command tree.

set -uo pipefail

TPTCTL="./bin/tptctl"
FAILURES=0

# Commands that legitimately have no counterpart in this repository. The SDK
# tutorial walks the reader through generating their own module, which brings
# its own build target and attaches its own plugin to the command line tool
# under a name only that reader's build knows about.
ALLOWED_TPTCTL_ROOTS="wordpress"
ALLOWED_MAGE_TARGETS="build:plugin"

# Section: preconditions
if [ ! -x "${TPTCTL}" ]; then
    echo "docs command check failed:"
    echo
    echo "${TPTCTL} was not found"
    echo "build it first with:"
    echo
    echo "  mage build:tptctl"

    exit 1
fi

# Section: collect every line inside a runnable fenced code block, once
# Only shell blocks and unlabelled blocks are read. A Go, YAML, JSON or TOML
# block can hold the same words in a comment without any of them being a
# command a reader is meant to type.
FENCED=$(mktemp)
trap 'rm -f "${FENCED}"' EXIT

find docs -name '*.md' -print0 \
    | xargs -0 awk '
        /^[[:space:]]*[`]{3}/ {
            # A closing fence carries no language, so track being inside a
            # fence separately from having chosen to read it. Otherwise the
            # line that closes a Go block reads as the start of a shell one.
            if (in_fence) { in_fence = 0; reading = 0; next }
            in_fence = 1
            language = $0
            sub(/^[[:space:]]*[`]{3}/, "", language)
            reading = (language == "" || language == "bash" || language == "sh" || language == "shell")
            next
        }
        reading { print }
      ' > "${FENCED}"

# Section: read the command tree out of the tool itself
# Prints the subcommand names of whatever command it is handed, and nothing
# when that command takes no subcommands.
subcommands_of() {
    "${TPTCTL}" "$@" --help 2>&1 \
        | sed -n '/Available Commands:/,/^$/p' \
        | awk 'NR > 1 && NF { print $1 }'
}

ROOT_COMMANDS=$(subcommands_of)

# Section: compare each documented invocation against that tree
# Anchored to the start of a line, optionally behind a shell prompt, so a
# command named in a sentence of narrative output is not read as an invocation.
INVOCATIONS=$(grep -oE '^[[:space:]]*[$]?[[:space:]]*tptctl [a-z][a-z0-9-]*( [a-z][a-z0-9-]*)?' "${FENCED}" \
    | sed -E 's/^[[:space:]]*[$]?[[:space:]]*//' \
    | sort -u)

while read -r _ verb object; do
    [ -z "${verb}" ] && continue

    if echo "${ALLOWED_TPTCTL_ROOTS}" | grep -qw "${verb}"; then
        continue
    fi

    if ! echo "${ROOT_COMMANDS}" | grep -qx "${verb}"; then
        echo "tptctl ${verb}: no such command"
        FAILURES=$((FAILURES + 1))
        continue
    fi

    if [ -z "${object}" ]; then
        continue
    fi

    # A command that offers no subcommands is driven entirely by flags, so a
    # bare word after it is swallowed as a stray argument and the reader gets
    # nothing they were promised.
    VERB_SUBCOMMANDS=$(subcommands_of "${verb}")
    if [ -z "${VERB_SUBCOMMANDS}" ]; then
        echo "tptctl ${verb} ${object}: ${verb} takes no subcommands"
        FAILURES=$((FAILURES + 1))
        continue
    fi

    if ! echo "${VERB_SUBCOMMANDS}" | grep -qx "${object}"; then
        echo "tptctl ${verb} ${object}: no such object type"
        FAILURES=$((FAILURES + 1))
    fi
done <<< "${INVOCATIONS}"

# Section: compare the documented build targets against the build system
MAKE_TARGETS=$(awk -F: '/^[a-zA-Z0-9][a-zA-Z0-9_-]*:/ { print $1 }' Makefile | sort -u)
MAGE_TARGETS=$(mage -l 2>/dev/null | awk 'NR > 1 && NF { print tolower($1) }' | sort -u)

for target in $(grep -oE '^[[:space:]]*[$]?[[:space:]]*make [a-zA-Z0-9][a-zA-Z0-9_-]*' "${FENCED}" | awk '{ print $NF }' | sort -u); do
    if ! echo "${MAKE_TARGETS}" | grep -qx "${target}"; then
        echo "make ${target}: no such target"
        FAILURES=$((FAILURES + 1))
    fi
done

for target in $(grep -oE '^[[:space:]]*[$]?[[:space:]]*mage [a-zA-Z0-9][a-zA-Z0-9:]*' "${FENCED}" | awk '{ print tolower($NF) }' | sort -u); do
    if echo "${ALLOWED_MAGE_TARGETS}" | tr ' ' '\n' | tr '[:upper:]' '[:lower:]' | grep -qx "${target}"; then
        continue
    fi

    if ! echo "${MAGE_TARGETS}" | grep -qx "${target}"; then
        echo "mage ${target}: no such target"
        FAILURES=$((FAILURES + 1))
    fi
done

# Section: verdict
if [ "${FAILURES}" -gt 0 ]; then
    echo
    echo "docs command check failed: ${FAILURES} command(s) in the documentation do not exist"

    exit 1
fi

echo "docs command check passed"
