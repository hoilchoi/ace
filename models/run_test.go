package models

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A poller reading a run while the runner rewrites it must always get a
// whole run.json, never an empty or half-written one.
func TestLoadRunNeverSeesAPartialSave(t *testing.T) {
	root := t.TempDir()
	run := &Run{ID: "r1", Scenario: "probe", Status: "running"}
	if err := os.MkdirAll(run.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run.Save(root); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if err := run.Save(root); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}()
	failures := 0
	for i := 0; i < 5000; i++ {
		if _, err := LoadRun(root, "r1"); err != nil {
			failures++
		}
	}
	close(stop)
	wg.Wait()
	if failures > 0 {
		t.Fatalf("%d of 5000 loads failed while the run was being saved", failures)
	}
}

// Operators read runs from the host (bind-mounted runs/); the container writes as root.
func TestSavedRunIsWorldReadable(t *testing.T) {
	root := t.TempDir()
	run := &Run{ID: "r1", Scenario: "probe", Status: "done"}
	if err := os.MkdirAll(run.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run.Save(root); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(run.Dir(root), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o044 != 0o044 {
		t.Fatalf("run.json mode %v, want group/other readable like before", fi.Mode().Perm())
	}
}
