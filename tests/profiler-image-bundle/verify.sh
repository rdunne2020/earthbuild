#!/usr/bin/env bash
set -euo pipefail

bundle_dir="$1"
cd "$bundle_dir"
jq -e '.schemaVersion == 1 and .compression == "zstd" and
    .archive == "images.tar.zst" and (.images | length) == 3 and
    all(.images[]; (.reference | length) > 0 and (.imageId | startswith("sha256:")))' manifest.json > /dev/null
sha256sum --check SHA256SUMS
zstd --test --quiet images.tar.zst
zstd --decompress --stdout images.tar.zst | tar -tf - > /tmp/bundle-contents.txt
if ! grep -qx 'manifest.json' /tmp/bundle-contents.txt &&
    ! grep -qx 'index.json' /tmp/bundle-contents.txt; then
    printf 'Image archive contains neither manifest.json nor index.json\n' >&2
    exit 1
fi
manifest_sha="$(jq -r .sha256 manifest.json)"
actual_sha="$(sha256sum images.tar.zst | cut -d ' ' -f 1)"
test "$manifest_sha" = "$actual_sha"
test "$(jq -r .sizeBytes manifest.json)" = "$(stat -c %s images.tar.zst)"
test -s archive-manifest.json
test -s probe-after.txt
test -s root-probe-after.txt
printf 'PROFILE_MARKER %s bundle-verified\n' "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)"
