# Profiling builds

Capture a build profile directly from EarthBuild:

```sh
earth --profile run.json +all
```

`EARTH_PROFILE=run.json` also enables profiling. Console output remains available
while the build runs. The profile is exported when the CLI finishes, including
on build failure and handled cancellation. Abrupt termination such as `SIGKILL`
cannot produce a final profile. The output directory must already exist.

The export uses the build's recorded lifecycle timestamps rather than the time
console lines arrive. It contains:

- A run manifest, with the build ID, status, targets, commands, source locations,
  platforms, cache flags, and dependency links.
- Run, target, and command durations in nanoseconds. A duration is `null` when
  either timestamp is missing or the timestamps are reversed. Zero is a valid
  duration, including for cached commands.
- Raw log chunks grouped by command ID, preserving arrival order within each
  command, the target ID, stream number, and recorded timestamp.

Target duration measures the recorded target lifecycle, which can include
conversion, waiting, and dependencies. Command duration measures the recorded
command lifecycle; BuildKit execution steps use solver timestamps. Parallel
durations overlap, so summing them does not give the build's elapsed time.
Commands without execution timestamps retain an unknown duration.

## Export format (version 1)

The JSON object contains `version`, `manifest`, `runDurationNanos`,
`targetDurationNanos`, `commandDurationNanos`, and `logs`. Duration maps are keyed
by the same IDs used in `manifest.targets` and `manifest.commands`. The manifest
uses the existing logstream protobuf JSON format, including decimal strings for
64-bit timestamps and named status enums. Durations and log timestamps are JSON
integers; consumers must preserve 64-bit precision.

Start at `manifest.mainTargetId` and follow each target's `dependsOn` IDs to show
the nested build graph. For example, `+all` can depend on `+build` and `+test`,
while `+test` also depends on the same `+build`. Preserve that shared node instead
of treating it as two executions. For the specific command that referenced a
target, use `manifest.commands[commandId].dependsOn`, which also records the
referenced name. IDs distinguish target instances with different build arguments
or platforms. These are dependency links, not a chronological function call stack.

Read `logs[commandId]` for that command's output. Each chunk has `targetId`,
`stream`, `timestampUnixNanos`, and `data`. `data` is base64 to preserve arbitrary
bytes and partial lines. Stream 1 is stdout, stream 2 is stderr, and other streams
can contain execution statistics. Decode text streams and concatenate chunks
before splitting lines. Generic CLI messages are grouped under their own command
IDs, which may have no command manifest entry. Commands with no output have no
entry in `logs`.

For example, list commands from slowest to fastest with `jq`:

```sh
jq -r '. as $p | .commandDurationNanos | to_entries
  | map(select(.value != null)) | sort_by(.value) | reverse[]
  | [.key, (.value / 1000000000), $p.manifest.commands[.key].name]
  | @tsv' run.json
```

The profile is written to a temporary file with mode `0600` and replaces the
destination after a successful export. Logs and build arguments can contain
sensitive application data. Profiling retains an additional copy of raw events
in memory until export, so large logs increase memory use. This format can be
ingested into a local database or used by a terminal viewer without parsing the
parallel console output.

## Development checks

```sh
go test ./logbus/profiler ./logbus/setup ./cmd/earth/flag ./cmd/earth/app
earth +lint
```
