package profiler

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/EarthBuild/earthbuild/logstream"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestProfile(t *testing.T) {
	t.Parallel()

	const (
		runID   = "run"
		allID   = "all"
		buildID = "build"
		testID  = "test"
	)

	for _, status := range []logstream.RunStatus{
		logstream.RunStatus_RUN_STATUS_SUCCESS, logstream.RunStatus_RUN_STATUS_FAILURE,
	} {
		t.Run(status.String(), func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "profile.json")
			initial := &logstream.RunManifest{BuildId: runID, Version: 2}

			r, err := New(path, initial)
			if err != nil {
				t.Fatal(err)
			}

			// Metadata can be discovered after profile setup.
			initial.IsCi = true

			r.Write(fields(&logstream.DeltaManifest_FieldsDelta{
				MainTargetId: allID, StartedAtUnixNanos: 100, EndedAtUnixNanos: 200, Status: status,
				Targets: map[string]*logstream.DeltaTargetManifest{
					allID:   {Name: "+all", DependsOn: []string{buildID, testID}},
					testID:  {Name: "+test", DependsOn: []string{buildID}},
					buildID: {Name: "+build", StartedAtUnixNanos: 110, EndedAtUnixNanos: 190},
				},
				Commands: map[string]*logstream.DeltaCommandManifest{
					runID: {
						Name: "RUN echo hello", TargetId: buildID, StartedAtUnixNanos: 120,
					},
					"cached": {
						Name: "RUN true", TargetId: buildID, HasCached: true, IsCached: true,
						StartedAtUnixNanos: 120, EndedAtUnixNanos: 120,
					},
					"unfinished": {TargetId: testID, StartedAtUnixNanos: 150},
					"call": {TargetId: allID, DependsOn: []*logstream.CommandTarget{
						{TargetId: buildID, ReferencedName: "+build"},
					}},
				},
			}))

			for _, id := range []string{runID, "cached", runID} {
				data := []byte{0, 255, 'a', '\n'}
				r.Write(&logstream.Delta{DeltaTypeOneof: &logstream.Delta_DeltaLog{DeltaLog: &logstream.DeltaLog{
					TargetId: buildID, CommandId: id, Stream: 2, TimestampUnixNanos: 140, Data: data,
				}}})
				data[0] = 42
			}

			r.Write(fields(&logstream.DeltaManifest_FieldsDelta{Commands: map[string]*logstream.DeltaCommandManifest{
				runID: {EndedAtUnixNanos: 180, Status: status},
			}}))

			err = r.Close()
			if err != nil {
				t.Fatal(err)
			}

			err = r.Close()
			if err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(path) // #nosec G304 -- path is inside t.TempDir().
			if err != nil {
				t.Fatal(err)
			}

			var p report

			err = json.Unmarshal(data, &p)
			if err != nil {
				t.Fatal(err)
			}

			var m logstream.RunManifest

			err = protojson.Unmarshal(p.Manifest, &m)
			if err != nil {
				t.Fatal(err)
			}

			if p.Version != 1 || *p.RunDuration != 100 || *p.CommandDurations[runID] != 60 ||
				*p.TargetDurations[buildID] != 80 || *p.CommandDurations["cached"] != 0 ||
				p.CommandDurations["unfinished"] != nil {
				t.Fatalf("wrong profile timings: %+v", p)
			}

			if m.GetStatus() != status || !m.GetIsCi() || !m.GetCommands()["cached"].GetIsCached() ||
				len(m.GetTargets()[allID].GetDependsOn()) != 2 ||
				m.GetCommands()["call"].GetDependsOn()[0].GetTargetId() != buildID {
				t.Fatalf("lost manifest metadata: %v", &m)
			}

			if len(p.Logs[runID]) != 2 || len(p.Logs["cached"]) != 1 ||
				p.Logs[runID][0].Data[0] != 0 || p.Logs[runID][0].Stream != 2 {
				t.Fatalf("lost or mixed command logs: %+v", p.Logs)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			if info.Mode().Perm() != 0o600 {
				t.Fatalf("profile permissions = %o, want 600", info.Mode().Perm())
			}
		})
	}
}

func TestInvalidEventPreservesExistingProfile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "profile.json")

	err := os.WriteFile(path, []byte("previous"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	r, err := New(path, &logstream.RunManifest{Version: 2})
	if err != nil {
		t.Fatal(err)
	}

	r.Write(&logstream.Delta{Version: 999})

	err = r.Close()
	if err == nil {
		t.Fatal("want unsupported version error")
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is inside t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "previous" {
		t.Fatalf("existing profile changed: %q", data)
	}

	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".earth-profile-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files remain: %v, %v", files, err)
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()

	for _, times := range [][2]uint64{{0, 10}, {10, 0}, {10, 9}} {
		if got := duration(times[0], times[1]); got != nil {
			t.Fatalf("duration(%v) = %d, want unknown", times, *got)
		}
	}
}

func fields(f *logstream.DeltaManifest_FieldsDelta) *logstream.Delta {
	return &logstream.Delta{DeltaTypeOneof: &logstream.Delta_DeltaManifest{DeltaManifest: &logstream.DeltaManifest{
		DeltaManifestOneof: &logstream.DeltaManifest_Fields{Fields: f},
	}}}
}
