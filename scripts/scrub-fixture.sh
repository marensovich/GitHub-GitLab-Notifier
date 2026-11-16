#!/usr/bin/env bash
# Prepares a real webhook payload for use as a test fixture.
#
#   scripts/scrub-fixture.sh payload.json > internal/handler/testdata/github/push/<case>.json
#
# Copy the payload from GitHub (Settings → Webhooks → Recent Deliveries) or GitLab
# (Settings → Webhooks → Edit → Recent events) and run it through this script. It:
#   - replaces every e-mail address with user@example.com;
#   - drops fields the bot never reads and that identify accounts or installations:
#     avatar URLs, node IDs, gravatar IDs, "installation", "organization" and "enterprise";
#   - pretty-prints the result with two-space indentation.
# Names, logins and URLs are kept because messages show them: review the output and
# replace anything private (repository, people, commit messages) by hand before committing.
set -euo pipefail

if [[ $# -ne 1 || ! -f "$1" ]]; then
  echo "usage: $0 <payload.json>" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required: https://jqlang.org/download/" >&2
  exit 1
fi

jq --indent 2 '
  del(.installation, .organization, .enterprise)
  | walk(
      if type == "object" then
        del(.avatar_url, .node_id, .gravatar_id)
      elif type == "string" then
        gsub("[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}"; "user@example.com")
      else . end
    )
' "$1"
