#!/usr/bin/env bash
# Publish one per-platform add-in bundle to the Oblikovati add-in catalogue (#1164).
#
# A bundle is a zip of the add-in folder: manifest.json + the shared library + icon.svg (+ any
# declared images). The manifest's apiCompat is stamped with the API major.minor the library
# was built against, so the catalogue serves this build only to a host on that API version.
#
# Usage: scripts/publish-catalogue.sh <platform> <library-file> <api-major.minor>
#   platform        one of: linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64
#   library-file    the built c-shared library (.so/.dylib/.dll)
#   api-major.minor e.g. 0.85
# Env:
#   ADDINS_PUBLISH_URL    catalogue base URL (default https://addins.oblikovati.org)
#   ADDINS_PUBLISH_TOKEN  this add-in's publish token (required)
set -euo pipefail

platform="${1:?platform (e.g. linux-amd64)}"
lib="${2:?path to the built shared library}"
api="${3:?API compatibility major.minor (e.g. 0.85)}"
url="${ADDINS_PUBLISH_URL:-https://addins.oblikovati.org}"
: "${ADDINS_PUBLISH_TOKEN:?ADDINS_PUBLISH_TOKEN is required}"

for f in manifest.json icon.svg "$lib"; do
  [ -f "$f" ] || { echo "publish-catalogue: $f not found" >&2; exit 1; }
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp icon.svg "$work/"
cp "$lib" "$work/"
# Stamp the manifest copy with the API version this binary was built against.
python3 - "manifest.json" "$work/manifest.json" "$api" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
m["apiCompat"] = sys.argv[3]
json.dump(m, open(sys.argv[2], "w"), indent=2)
PY
name="$(python3 -c 'import json;print(json.load(open("manifest.json"))["id"])')"
( cd "$work" && zip -qr bundle.zip . )

echo "publish-catalogue: $name $platform (apiCompat $api) -> $url"
curl -fsS -X POST "$url/publish" \
  -H "Authorization: Bearer $ADDINS_PUBLISH_TOKEN" \
  -F "name=$name" \
  -F "platform=$platform" \
  -F "bundle=@$work/bundle.zip"
echo
