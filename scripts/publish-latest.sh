#!/bin/sh
set -eu

BUCKET=${QODO_SUPPORT_BUNDLE_DESTINATION_BUCKET:-qodo-cli-public}
PREFIX=${QODO_SUPPORT_BUNDLE_PREFIX:-support-bundle}
VERSION=${QODO_SUPPORT_BUNDLE_VERSION:?QODO_SUPPORT_BUNDLE_VERSION is required}
RELEASE_DIR=${QODO_SUPPORT_BUNDLE_RELEASE_DIR:?QODO_SUPPORT_BUNDLE_RELEASE_DIR is required}
ROOT=$(unset CDPATH; cd "$(dirname "$0")/.." && pwd)
. "$ROOT/scripts/gcs-exact-object.sh"
. "$ROOT/scripts/release-contract.sh"

stable_version() {
  awk '
    { value = $0; valid = ($0 ~ /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/) }
    END { if (NR != 1 || !valid || length(value) > 63) exit 1; print value }
  '
}

printf '%s\n' "$VERSION" | stable_version >/dev/null || {
  echo 'publish-latest: only stable release versions can become latest' >&2
  exit 1
}
gcs_validate_name "$BUCKET" "$PREFIX/latest-version.txt" publish-latest
validate_release_directory "$RELEASE_DIR" publish-latest
for INSTALLER in install.sh install.ps1; do
  grep -F 'latest-version.txt' "$RELEASE_DIR/$INSTALLER" >/dev/null || {
    echo "publish-latest: $INSTALLER does not support latest discovery" >&2
    exit 1
  }
done

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
TOKEN=$(gcloud auth print-access-token)
release_files | while IFS= read -r FILE; do
  gcs_verify_exact \
    "$RELEASE_DIR/$FILE" "$BUCKET" "$PREFIX/releases/$VERSION/$FILE" \
    "$(release_content_type "$FILE")" 'public, max-age=31536000, immutable' \
    "$TOKEN" "$WORK/release-existing" publish-latest
done

LATEST_GENERATION=0
if gcs_read_exact "$BUCKET" "$PREFIX/latest-version.txt" "$TOKEN" \
  "$WORK/current-version" "$WORK/current-headers" publish-latest; then
  CURRENT=$(stable_version < "$WORK/current-version") || {
    echo 'publish-latest: existing latest version is invalid' >&2
    exit 1
  }
  NEWEST=$(printf '%s\n%s\n' "$CURRENT" "$VERSION" | LC_ALL=C sort -V | tail -n 1)
  [ "$NEWEST" = "$VERSION" ] || {
    echo "publish-latest: refusing to downgrade latest from $CURRENT to $VERSION" >&2
    exit 1
  }
  LATEST_GENERATION=$(gcs_header_value x-goog-generation "$WORK/current-headers")
else
  READ_STATUS=$?
  [ "$READ_STATUS" -eq 1 ] || exit "$READ_STATUS"
fi

update_discovery() (
  DISCOVERY_FILE=$1
  DISCOVERY_SOURCE=$2
  DISCOVERY_GENERATION=${3-}
  case "$DISCOVERY_FILE" in
    install.sh|install.ps1|latest-version.txt) ;;
    *) exit 1 ;;
  esac
  if [ -z "$DISCOVERY_GENERATION" ]; then
    DISCOVERY_GENERATION=0
    if gcs_read_exact "$BUCKET" "$PREFIX/$DISCOVERY_FILE" "$TOKEN" \
      "$WORK/existing" "$WORK/headers" publish-latest; then
      if gcs_object_matches "$DISCOVERY_SOURCE" "$WORK/existing" "$WORK/headers" \
        'text/plain; charset=utf-8' no-store; then
        exit 0
      fi
      DISCOVERY_GENERATION=$(gcs_header_value x-goog-generation "$WORK/headers")
    else
      READ_STATUS=$?
      [ "$READ_STATUS" -eq 1 ] || exit "$READ_STATUS"
    fi
  fi
  case "$DISCOVERY_GENERATION" in
    ''|*[!0-9]*) echo 'publish-latest: missing object generation' >&2; exit 1 ;;
  esac
  gcs_conditional_put "$DISCOVERY_SOURCE" "$BUCKET" "$PREFIX/$DISCOVERY_FILE" \
    'text/plain; charset=utf-8' no-store "$DISCOVERY_GENERATION" "$TOKEN" publish-latest
  gcs_verify_exact "$DISCOVERY_SOURCE" "$BUCKET" "$PREFIX/$DISCOVERY_FILE" \
    'text/plain; charset=utf-8' no-store "$TOKEN" "$WORK/verified" publish-latest
)

for FILE in install.sh install.ps1; do
  update_discovery "$FILE" "$RELEASE_DIR/$FILE"
done
printf '%s\n' "$VERSION" > "$WORK/latest-version.txt"
# Bootstrap scripts resolve any supported release; only the pointer selects the installed version.
update_discovery latest-version.txt "$WORK/latest-version.txt" "$LATEST_GENERATION"
printf 'publish-latest: latest production release is %s\n' "$VERSION"
