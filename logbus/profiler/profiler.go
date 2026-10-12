// Package profiler exports build timings, dependency links, and command logs.
package profiler

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/EarthBuild/earthbuild/logstream"
	"github.com/EarthBuild/earthbuild/util/deltautil"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Recorder subscribes to raw build events and exports a versioned JSON profile.
type Recorder struct {
	manifest *logstream.RunManifest
	initial  *logstream.RunManifest
	logs     map[string][]logChunk
	file     *os.File
	err      error
	path     string
	mu       sync.Mutex
	closed   bool
}

type logChunk struct {
	TargetID           string `json:"targetId"`
	Data               []byte `json:"data"`
	Stream             int32  `json:"stream"`
	TimestampUnixNanos uint64 `json:"timestampUnixNanos"`
}

type report struct {
	RunDuration      *uint64               `json:"runDurationNanos"`
	TargetDurations  map[string]*uint64    `json:"targetDurationNanos"`
	CommandDurations map[string]*uint64    `json:"commandDurationNanos"`
	Logs             map[string][]logChunk `json:"logs"`
	Manifest         jsontext.Value        `json:"manifest"`
	Version          int                   `json:"version"`
}

// New reserves a private temporary file beside path. The profile replaces path
// only after a complete export. initial supplies the build ID and creation time.
func New(path string, initial *logstream.RunManifest) (*Recorder, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".earth-profile-*")
	if err != nil {
		return nil, fmt.Errorf("create profile: %w", err)
	}

	manifest := &logstream.RunManifest{}
	proto.Merge(manifest, initial)

	return &Recorder{
		manifest: manifest,
		initial:  initial,
		logs:     make(map[string][]logChunk),
		file:     f,
		path:     path,
	}, nil
}

// Write records a raw manifest delta or log chunk. Formatted logs are ignored.
func (r *Recorder) Write(d *logstream.Delta) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.err != nil {
		return
	}
	// Delta application retains pointers, so own the data independently of the bus.
	owned := &logstream.Delta{}
	proto.Merge(owned, d)

	err := deltautil.ApplyDelta(r.manifest, owned)
	if err != nil {
		r.err = err
		return
	}

	if l := owned.GetDeltaLog(); l != nil {
		r.logs[l.GetCommandId()] = append(r.logs[l.GetCommandId()], logChunk{
			TargetID: l.GetTargetId(), Stream: l.GetStream(),
			TimestampUnixNanos: l.GetTimestampUnixNanos(), Data: l.GetData(),
		})
	}
}

// Close writes the final profile and releases the temporary file. It is idempotent.
// Call after the run's terminal status has been recorded, including on failure.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return r.err
	}

	r.closed = true
	defer os.Remove(r.file.Name())

	if r.err == nil {
		r.err = r.export()
	}

	r.err = errors.Join(r.err, r.file.Close())
	if r.err == nil {
		r.err = os.Rename(r.file.Name(), r.path)
	}

	return r.err
}

func (r *Recorder) export() error {
	proto.Merge(r.manifest, r.initial)

	manifest, err := protojson.Marshal(r.manifest)
	if err != nil {
		return fmt.Errorf("marshal profile manifest: %w", err)
	}

	p := report{
		Version: 1, Manifest: manifest, Logs: r.logs,
		RunDuration:      duration(r.manifest.GetStartedAtUnixNanos(), r.manifest.GetEndedAtUnixNanos()),
		TargetDurations:  make(map[string]*uint64),
		CommandDurations: make(map[string]*uint64),
	}
	for id, t := range r.manifest.GetTargets() {
		p.TargetDurations[id] = duration(t.GetStartedAtUnixNanos(), t.GetEndedAtUnixNanos())
	}

	for id, c := range r.manifest.GetCommands() {
		p.CommandDurations[id] = duration(c.GetStartedAtUnixNanos(), c.GetEndedAtUnixNanos())
	}

	err = json.MarshalWrite(r.file, p)
	if err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	return r.file.Sync()
}

func duration(start, end uint64) *uint64 {
	if start == 0 || end == 0 || end < start {
		return nil
	}

	d := end - start

	return &d
}
