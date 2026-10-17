package profiler

import (
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
)

func TestExecutionIntervals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want          *uint64
		name          string
		vertices      []client.Vertex
		wantCached    bool
		wantIntervals int
	}{
		{
			name: "copy followed by a delayed cache replay",
			vertices: []client.Vertex{
				vertex("copy", 100, 0, false),
				vertex("copy", 100, 110, false),
				vertex("copy", 840100, 840101, true),
				vertex("copy", 840100, 840101, true),
			},
			want: new(uint64(10)), wantIntervals: 2,
		},
		{
			name:     "later snapshot of the same interval",
			vertices: []client.Vertex{vertex("copy", 100, 110, false), vertex("copy", 100, 840101, true)},
			want:     new(uint64(10)), wantIntervals: 1,
		},
		{
			name:     "distinct real executions exclude their idle gap",
			vertices: []client.Vertex{vertex("run", 100, 110, false), vertex("run", 10000, 10020, false)},
			want:     new(uint64(30)), wantIntervals: 2,
		},
		{
			name:     "different vertices sharing a command do not double count overlap",
			vertices: []client.Vertex{vertex("file", 100, 120, false), vertex("merge", 110, 130, false)},
			want:     new(uint64(30)), wantIntervals: 2,
		},
		{
			name:     "cache only including lookup completion update",
			vertices: []client.Vertex{vertex("cached", 100, 0, false), vertex("cached", 100, 105, true)},
			want:     new(uint64(5)), wantCached: true, wantIntervals: 1,
		},
		{
			name:     "cache lookup followed by actual execution",
			vertices: []client.Vertex{vertex("run", 100, 105, true), vertex("run", 200, 220, false)},
			want:     new(uint64(20)), wantIntervals: 2,
		},
		{
			name:          "unfinished execution remains unknown",
			vertices:      []client.Vertex{vertex("run", 100, 110, false), vertex("run", 200, 0, false)},
			wantIntervals: 2,
		},
		{
			name:          "missing start remains unknown",
			vertices:      []client.Vertex{vertex("run", 0, 110, false)},
			wantIntervals: 1,
		},
		{
			name:          "reversed timestamps remain unknown",
			vertices:      []client.Vertex{vertex("run", 100, 90, false)},
			wantIntervals: 1,
		},
		{
			name:     "zero duration is valid",
			vertices: []client.Vertex{vertex("cached", 100, 100, true)},
			want:     new(uint64(0)), wantCached: true, wantIntervals: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Recorder{intervals: make(map[string]map[intervalKey]*executionInterval)}
			for _, snapshot := range tt.vertices {
				r.ObserveVertex("command", &snapshot)
			}

			all := orderedIntervals(r.intervals["command"])

			got, cached := executionDuration(all)
			if len(all) != tt.wantIntervals || cached != tt.wantCached {
				t.Fatalf("intervals = %+v, cached = %v; want %d intervals, cached %v", all, cached, tt.wantIntervals, tt.wantCached)
			}

			if (got == nil) != (tt.want == nil) {
				t.Fatalf("duration = %v, want %v", got, tt.want)
			}

			if got != nil && *got != *tt.want {
				t.Fatalf("duration = %d, want %d", *got, *tt.want)
			}
		})
	}
}

func vertex(id string, start, end int64, cached bool) client.Vertex {
	v := client.Vertex{Digest: digest.FromString(id), Cached: cached}
	if start != 0 {
		v.Started = new(time.Unix(0, start))
	}

	if end != 0 {
		v.Completed = new(time.Unix(0, end))
	}

	return v
}
