package profiler

import (
	"cmp"
	"slices"
	"time"

	"github.com/moby/buildkit/client"
)

type intervalKey struct {
	vertexID string
	start    uint64
}

type executionInterval struct {
	VertexID  string `json:"vertexId"`
	StartedAt uint64 `json:"startedAtUnixNanos"`
	EndedAt   uint64 `json:"endedAtUnixNanos"`
	Cached    bool   `json:"isCached"`
}

// ObserveVertex records a solver interval before logbus suppresses later starts.
// Each vertex digest and start timestamp identifies a separate interval. The
// first completion of that interval is retained when snapshots are replayed.
func (r *Recorder) ObserveVertex(commandID string, vertex *client.Vertex) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || vertex.Started == nil && vertex.Completed == nil {
		return
	}

	key := intervalKey{vertexID: vertex.Digest.String(), start: unixNanos(vertex.Started)}

	intervals := r.intervals[commandID]
	if intervals == nil {
		intervals = make(map[intervalKey]*executionInterval)
		r.intervals[commandID] = intervals
	}

	interval := intervals[key]
	if interval == nil {
		interval = &executionInterval{VertexID: key.vertexID, StartedAt: key.start}
		intervals[key] = interval
	}

	if interval.EndedAt != 0 {
		return
	}

	interval.Cached = vertex.Cached
	interval.EndedAt = unixNanos(vertex.Completed)
}

func unixNanos(t *time.Time) uint64 {
	if t == nil || t.UnixNano() <= 0 {
		return 0
	}

	return uint64(t.UnixNano()) // #nosec G115 -- the timestamp is positive.
}

func orderedIntervals(intervals map[intervalKey]*executionInterval) []executionInterval {
	all := make([]executionInterval, 0, len(intervals))
	for _, interval := range intervals {
		all = append(all, *interval)
	}

	slices.SortFunc(all, func(a, b executionInterval) int {
		if c := cmp.Compare(a.StartedAt, b.StartedAt); c != 0 {
			return c
		}

		if c := cmp.Compare(a.EndedAt, b.EndedAt); c != 0 {
			return c
		}

		return cmp.Compare(a.VertexID, b.VertexID)
	})

	return all
}

// executionDuration measures the union of non-cached intervals, excluding idle
// gaps and overlapping work. For a cache-only command it measures cache lookup
// intervals instead. Missing or invalid timestamps produce an unknown duration.
func executionDuration(all []executionInterval) (*uint64, bool) {
	cachedOnly := true
	for _, interval := range all {
		cachedOnly = cachedOnly && interval.Cached
	}

	var total, start, end uint64

	for _, interval := range all {
		if !cachedOnly && interval.Cached {
			continue
		}

		if duration(interval.StartedAt, interval.EndedAt) == nil {
			return nil, cachedOnly
		}

		if start == 0 {
			start, end = interval.StartedAt, interval.EndedAt

			continue
		}

		if interval.StartedAt > end {
			total += end - start
			start, end = interval.StartedAt, interval.EndedAt

			continue
		}

		end = max(end, interval.EndedAt)
	}

	if start == 0 {
		return nil, cachedOnly
	}

	total += end - start

	return &total, cachedOnly
}
