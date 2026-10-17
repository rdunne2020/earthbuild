#!/usr/bin/env python3
"""Summarize step spans and repeated completion deltas in a profile capture."""

import argparse
import base64
import collections
import json
from pathlib import Path


def seconds(value):
    return "unknown" if value is None else f"{int(value) / 1_000_000_000:.6f}s"


def summarize(profile, deltas):
    manifest = profile.get("manifest", {})
    commands = manifest.get("commands", {})
    durations = profile.get("commandDurationNanos", {})
    run_start = int(manifest.get("startedAtUnixNanos", 0))
    starts = collections.defaultdict(list)
    ends = collections.defaultdict(list)
    for delta in deltas:
        fields = delta.get("deltaManifest", {}).get("fields", {})
        for command_id, change in fields.get("commands", {}).items():
            if change.get("startedAtUnixNanos"):
                starts[command_id].append(int(change["startedAtUnixNanos"]))
            if change.get("endedAtUnixNanos"):
                ends[command_id].append(int(change["endedAtUnixNanos"]))

    lines = [
        f"Build: {manifest.get('buildId', '(missing)')}",
        f"Status: {manifest.get('status', '(missing)')}",
        f"Run span: {seconds(profile.get('runDurationNanos'))}",
        "",
        "COPY timings (execution intervals when available; otherwise lifecycle spans):",
    ]
    for command_id, command in sorted(commands.items()):
        if not command.get("name", "").startswith(("COPY ", "LINK COPY ")):
            continue
        start = int(command.get("startedAtUnixNanos", 0))
        end = int(command.get("endedAtUnixNanos", 0))
        first_end = min(ends[command_id], default=0)
        first_interval = first_end - start if start and first_end >= start else None
        recorded_intervals = profile.get("executionIntervals", {}).get(command_id, [])
        timing_kind = profile.get("commandTimingKinds", {}).get(command_id, "lifecycle")
        manifest_span = end - start if start and end >= start else None
        lines.extend([
            f"  {command.get('name', command_id)} [{command_id}]",
            f"    span={seconds(durations.get(command_id))} "
            f"start-offset={seconds(start - run_start) if start else 'unknown'} "
            f"end-offset={seconds(end - run_start) if end else 'unknown'} "
            f"cached={command.get('isCached', False)}",
            f"    timing-kind={timing_kind} solver-intervals={len(recorded_intervals)} "
            f"merged-manifest-span={seconds(manifest_span)}",
            f"    first-recorded-interval={seconds(first_interval)}",
            f"    start-deltas={len(starts[command_id])} "
            f"end-deltas={len(ends[command_id])} "
            f"distinct-end-times={len(set(ends[command_id]))}",
        ])

    lines.extend(["", "Commands with multiple distinct completion timestamps:"])
    repeated = [command_id for command_id, times in ends.items() if len(set(times)) > 1]
    if not repeated:
        lines.append("  None in this capture.")
    for command_id in sorted(repeated):
        command = commands.get(command_id, {})
        offsets = ", ".join(seconds(end - run_start) for end in sorted(set(ends[command_id])))
        lines.append(f"  {command.get('name', command_id)} [{command_id}]: {offsets}")

    lines.extend(["", "Slowest recorded command spans:"])
    ranked = sorted(
        ((command_id, int(span)) for command_id, span in durations.items() if span is not None),
        key=lambda item: (-item[1], item[0]),
    )
    for command_id, span in ranked[:15]:
        lines.append(f"  {seconds(span):>15}  {commands.get(command_id, {}).get('name', command_id)}")

    lines.extend(["", "Workload markers (from recorded command output):"])
    for command_id, chunks in profile.get("logs", {}).items():
        output = b"".join(base64.b64decode(chunk.get("data", "")) for chunk in chunks if chunk.get("stream") == 1)
        for line in output.decode("utf-8", errors="replace").splitlines():
            if line.startswith("PROFILE_MARKER "):
                lines.append(f"  [{command_id}] {line}")
    lines.extend([
        "",
        "The delta stream is captured after logbus processing. Later starts may",
        "already have been suppressed; distinct completion times are a diagnostic",
        "signal, not proof of an execution retry or a measurement of active time.",
    ])
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", type=Path)
    parser.add_argument("deltas", type=Path)
    args = parser.parse_args()
    with args.profile.open(encoding="utf-8") as stream:
        profile = json.load(stream)
    with args.deltas.open(encoding="utf-8") as stream:
        deltas = (json.loads(line) for line in stream if line.strip())
        print(summarize(profile, deltas))


if __name__ == "__main__":
    main()
