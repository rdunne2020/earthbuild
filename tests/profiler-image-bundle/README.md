# Image bundle profiling reproduction

This workload pulls three public container images, saves them together with
`docker image save`, compresses the archive with zstd, and generates an external
manifest mapping each requested reference to its image ID, available repository
digests, platform, and size. It validates the compressed archive and checksum.

A tiny shared `+note` artifact is copied before and after bundling, both inside
`+bundle` and in its parent `+all`. A controlled delay makes it easier to detect
an earlier COPY span incorrectly absorbing later work. This is a diagnostic
workload, not a guarantee of reproducing an inflated duration.

## Run and capture

From the repository root, using the profiling binary built earlier:

```sh
tests/profiler-image-bundle/capture.sh
```

The script first runs `earth ./buildkitd+buildkitd` with a dedicated image tag,
compiling the checkout's pinned BuildKit revision. It then runs the workload with
that freshly built image, a separate installation name, and a separate daemon
container (`earth-profiler-repro-buildkitd`). It needs Docker, network access,
Python 3 on the host, and permission to run privileged containers. The build's
normal output remains visible. It does not push images to a registry.

Each invocation creates a private directory under `build/profile-repro/`:

- `daemon-build.log`: the daemon compilation log.
- `console.log`: the workload's console output.
- `profile.json`: the profile for import into the TUI.
- `deltas.json`: one logbus event per JSON line, including raw and formatted logs.
- `manifest.json`: the final logstream run manifest, separate from the bundle manifest.
- `timing-summary.txt`: COPY spans, repeated completion timestamps, and phase markers.
- `artifacts/`: `images.tar.zst`, its `manifest.json` and `SHA256SUMS`, the archive's
  own manifest, image inspection records, archive contents, and COPY probe files.

The daemon container and its build cache remain available after the run.
The script uses an existing bootstrap `earth` to compile the daemon and defaults
to `build/profile/earth` for the actual workload. Override either if needed:

```sh
EARTH_BIN=/absolute/path/to/earth BOOTSTRAP_EARTH=/absolute/path/to/bootstrap-earth \
    tests/profiler-image-bundle/capture.sh
```

Default images are Alpine 3.24.2, BusyBox 1.37.0, and Redis 7.4 Alpine. Image tags
are resolved at runtime; the bundle manifest records the image IDs actually used.
The first run also downloads the Docker-in-Docker base image and build tools.
Defaults use zstd level 3 with two threads and a 15-second artificial delay.
Arguments after the script name are EarthBuild build arguments:

```sh
# A fourteen-minute controlled gap, without fourteen minutes of busy CPU work.
tests/profiler-image-bundle/capture.sh --HOLD_SECONDS=840

# Substitute images or increase compression work.
tests/profiler-image-bundle/capture.sh \
    --IMAGE_C=docker.io/library/postgres:17-alpine \
    --ZSTD_LEVEL=19 --ZSTD_THREADS=2 --HOLD_SECONDS=30
```

## Flags already available in the test binary

`--logstream-debug-file`, `--logstream-debug-manifest-file`, and `--profile` are
already implemented. The first two are hidden flags. A `.json` suffix is required
for JSON encoding; another suffix selects protobuf output. To show hidden flags:

```sh
EARTH_SHOW_HIDDEN=true build/profile/earth --help
```

The capture script supplies all three flags, plus `--allow-privileged`, `--no-cache`, and artifact mode,
so the bundle and traces come from the same build invocation.

The event dump is **after logbus processing**. It can show later completion
updates for a command, but later starts may already have been suppressed by
`Command.SetStart`. It does not preserve raw BuildKit vertex digests or attempt
identity. Multiple distinct end timestamps are evidence worth investigating,
not proof of an execution retry. Do not interpret these spans as active time.

## Inspect a capture

Replace `CAPTURE` with the directory printed by the script:

```sh
python3 tests/profiler-image-bundle/analyze.py CAPTURE/profile.json CAPTURE/deltas.json
/home/scrooge/Code/earthbuild-profiler/bin/earth-profile import CAPTURE/profile.json
/home/scrooge/Code/earthbuild-profiler/bin/earth-profile browse
```

Use the TUI's `s` key for longest recorded spans and select a `COPY` to inspect
its source location. The `PROFILE_MARKER` lines delimit image preparation, image
saving, the artificial delay, compression, manifest generation, and verification.

## Development checks

```sh
bash -n tests/profiler-image-bundle/{bundle,verify,capture}.sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
    -s tests/profiler-image-bundle -p 'test_*.py'
earth +lint
```

## Observed reproduction

A completed run on October 17, 2026 took 32.17 seconds with the default
15-second delay. The small `COPY +note/note.txt /work/probe-before.txt`
started at run offset 7.149737 seconds and first completed at 7.270217 seconds
(an interval of about 120 milliseconds). Its command ID then received completion
updates at offsets 29.606982, 30.162640, and 31.444470 seconds. The exported
profile reported 24.294733 seconds for that COPY, using the first start and the
last completion. This reproduces the inflated elapsed span without a long run.

The same run produced a verified 21 MiB compressed archive containing all three
images. These numbers describe one diagnostic run; network and host timings vary.

With the updated CLI's solver interval capture, a fresh run took 34.17 seconds.
The same small COPY had a merged manifest span of 25.079181 seconds, but its
exported `commandDurationNanos` correctly reported 0.212969 seconds. The profile
retained four solver intervals for that command and did not relabel its actual
execution as cached. The archive and checksum checks passed, all 28 solver-backed
commands had complete interval timings, and the existing TUI imported the profile.

The raw logbus delta stream can still contain later completion updates. Corrected
profiles use the separately observed solver intervals; this does not rewrite
historical lifecycle events. See the [profiling guide](../../docs/guides/profiling.md)
for `executionIntervals` and `commandTimingKinds`. Older profiles need a new run
with the updated CLI to obtain these timings.
