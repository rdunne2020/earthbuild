#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
earth_bin="${EARTH_BIN:-$repo_root/build/profile/earth}"
bootstrap_earth="${BOOTSTRAP_EARTH:-earth}"
capture_dir="$repo_root/build/profile-repro/$(date -u +%Y%m%dT%H%M%SZ)-$$"
image='ghcr.io/earthbuild/earthbuild:buildkitd-profiler-repro'

if [[ ! -x "$earth_bin" ]]; then
    printf 'Build the profiling CLI first, or set EARTH_BIN to its absolute path: %s\n' "$earth_bin" >&2
    exit 2
fi
mkdir -p "$capture_dir"
printf 'Capture directory: %s\n' "$capture_dir"
cd "$repo_root"

# Always compile the daemon from this checkout's pinned BuildKit revision.
# Keep bootstrap build events out of the workload's profile and delta stream.
"$bootstrap_earth" ./buildkitd+buildkitd --TAG=profiler-repro --RELEASE_VERSION=dev-profile \
    > "$capture_dir/daemon-build.log" 2>&1 || {
        printf 'Daemon build failed. See %s/daemon-build.log\n' "$capture_dir" >&2
        exit 1
    }

# Artifact mode exports /out from this same run, without invoking a second build.
set +e
"$earth_bin" \
    --installation-name earth-profiler-repro \
    --buildkit-container-name earth-profiler-repro-buildkitd \
    --buildkit-host docker-container://earth-profiler-repro-buildkitd \
    --buildkit-image "$image" \
    --allow-privileged \
    --no-cache \
    --profile "$capture_dir/profile.json" \
    --logstream-debug-file "$capture_dir/deltas.json" \
    --logstream-debug-manifest-file "$capture_dir/manifest.json" \
    --artifact ./tests/profiler-image-bundle+all/out "$capture_dir/artifacts" "$@" \
    2>&1 | tee "$capture_dir/console.log"
build_status="${PIPESTATUS[0]}"
set -e
printf '\nCapture directory: %s\nBuild exit status: %s\n' "$capture_dir" "$build_status"
if [[ -f "$capture_dir/profile.json" ]]; then
    python3 "$script_dir/analyze.py" "$capture_dir/profile.json" "$capture_dir/deltas.json" \
        > "$capture_dir/timing-summary.txt"
    cat "$capture_dir/timing-summary.txt"
fi
exit "$build_status"
