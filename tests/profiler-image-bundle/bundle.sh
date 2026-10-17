#!/usr/bin/env bash
set -euo pipefail

hold_seconds="$1"
zstd_level="$2"
zstd_threads="$3"
shift 3

if [[ ! "$hold_seconds" =~ ^[0-9]+$ ]] ||
    [[ ! "$zstd_level" =~ ^([1-9]|1[0-9])$ ]] ||
    [[ ! "$zstd_threads" =~ ^[1-9][0-9]*$ ]]; then
    printf 'Use integer HOLD_SECONDS >= 0, ZSTD_LEVEL 1..19, and ZSTD_THREADS >= 1\n' >&2
    exit 2
fi

trace_marker() {
    printf 'PROFILE_MARKER %s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)" "$1"
}

mkdir -p /out
trace_marker images-ready
# WITH DOCKER --pull fetches the requested images before this RUN begins.
# Inspect each requested reference separately so tags are mapped to image IDs.
for image in "$@"; do
    docker image inspect "$image" |
        jq --arg reference "$image" '.[0] | {
            reference: $reference,
            imageId: .Id,
            repoDigests: (.RepoDigests // []),
            os: .Os,
            architecture: .Architecture,
            variant: (.Variant // ""),
            sizeBytes: .Size
        }' >> /out/image-inspections.jsonl
done
trace_marker save-start
docker image save --output /out/images.tar "$@"
trace_marker save-end

# A controlled gap makes it easy to check whether an earlier COPY improperly
# inherits time spent in a later operation. Set 840 for a fourteen-minute gap.
trace_marker hold-start
sleep "$hold_seconds"
trace_marker hold-end
trace_marker compress-start
zstd -T"$zstd_threads" -"$zstd_level" --no-progress /out/images.tar -o /out/images.tar.zst
trace_marker compress-end

# Preserve the archive's own manifest and write an external bundle manifest.
# The image save format can be either Docker archive or OCI layout.
tar -tf /out/images.tar > /out/archive-contents.txt
if grep -qx 'manifest.json' /out/archive-contents.txt; then
    tar -xOf /out/images.tar manifest.json > /out/archive-manifest.json
else
    tar -xOf /out/images.tar index.json > /out/archive-manifest.json
fi
bundle_sha256="$(sha256sum /out/images.tar.zst | cut -d ' ' -f 1)"
bundle_size="$(stat -c %s /out/images.tar.zst)"
jq -s --arg archive images.tar.zst --arg sha256 "$bundle_sha256" \
    --argjson size_bytes "$bundle_size" --argjson level "$zstd_level" \
    '{schemaVersion: 1, archive: $archive, compression: "zstd", compressionLevel: $level,
      sha256: $sha256, sizeBytes: $size_bytes, images: .}' \
    /out/image-inspections.jsonl > /out/manifest.json
(cd /out && sha256sum images.tar.zst > SHA256SUMS)
# The uncompressed archive is deliberately excluded from the exported artifacts.
rm /out/images.tar
trace_marker manifest-ready
jq '{archive, sizeBytes, imageCount: (.images | length), references: [.images[].reference]}' /out/manifest.json
