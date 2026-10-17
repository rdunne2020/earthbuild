package setup

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EarthBuild/earthbuild/logbus"
	"github.com/EarthBuild/earthbuild/logstream"
	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestProfileUsesSolverIntervalsBeforeCommandMerge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	bus := logbus.New()

	bs, err := New(ctx, bus, false, false, false, true, "", "build", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "profile.json")

	err = bs.EnableProfile(path)
	if err != nil {
		t.Fatal(err)
	}

	// A single COPY executes quickly, then a later solve reuses its result.
	// The manifest keeps the first start and the latest completion, but the
	// exported duration must retain the original execution interval.
	start := bus.CreatedAt().Add(time.Second)
	end := start.Add(120 * time.Millisecond)
	replayStart := start.Add(14 * time.Minute)
	replayEnd := replayStart.Add(time.Millisecond)

	const commandName = "COPY note.txt ."

	vertexDigest := digest.FromString(commandName)

	statuses := make(chan *client.SolveStatus, 3)
	for _, v := range []*client.Vertex{
		{Digest: vertexDigest, Name: commandName, Started: &start},
		{Digest: vertexDigest, Name: commandName, Started: &start, Completed: &end},
		{Digest: vertexDigest, Name: commandName, Started: &replayStart, Completed: &replayEnd, Cached: true},
	} {
		statuses <- &client.SolveStatus{Vertexes: []*client.Vertex{v}}
	}

	close(statuses)

	err = bs.SolverMonitor.MonitorProgress(ctx, statuses)
	if err != nil {
		t.Fatal(err)
	}

	err = bs.Close()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is inside t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	var report struct {
		Durations map[string]*uint64          `json:"commandDurationNanos"`
		Kinds     map[string]string           `json:"commandTimingKinds"`
		Intervals map[string][]jsontext.Value `json:"executionIntervals"`
		Manifest  jsontext.Value              `json:"manifest"`
	}

	err = json.Unmarshal(data, &report)
	if err != nil {
		t.Fatal(err)
	}

	id := vertexDigest.String()

	want := uint64(120 * time.Millisecond)
	if got := report.Durations[id]; got == nil || *got != want {
		t.Fatalf("COPY duration = %v, want %d ns", got, want)
	}

	if report.Kinds[id] != "execution" || len(report.Intervals[id]) != 2 {
		t.Fatalf("missing execution intervals: %+v", report)
	}

	var manifest logstream.RunManifest

	err = protojson.Unmarshal(report.Manifest, &manifest)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.GetCommands()[id].GetIsCached() {
		t.Fatal("a later cache replay relabeled actual execution as cached")
	}
}
