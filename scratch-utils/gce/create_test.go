// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// fakeCompute is the slice of the compute API Create uses. Each zone's insert
// either succeeds, conflicts, or never finishes; a slow one can leave a machine
// behind, as GCE did when it took longer than the per-zone deadline.
type fakeCompute struct {
	mu        sync.Mutex
	insert    map[string]string // zone -> "ok" | "conflict" | "slow" | "slow-landed"
	instances map[string]string // zone -> status
	inserts   []string
	deletes   []string
}

func (f *fakeCompute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// .../projects/p/zones/<zone>/<kind>[/<name>[/wait]]
	zoneAt := -1
	for i, p := range parts {
		if p == "zones" && i+1 < len(parts) {
			zoneAt = i + 1
			break
		}
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case strings.Contains(r.URL.Path, "/aggregated/instances"):
		items := map[string]compute.InstancesScopedList{}
		for z, st := range f.instances {
			items["zones/"+z] = compute.InstancesScopedList{Instances: []*compute.Instance{{Name: "vm", Status: st}}}
		}
		reply(compute.InstanceAggregatedList{Items: items})
	case zoneAt > 0 && parts[zoneAt+1] == "operations":
		op := parts[zoneAt+2]
		if strings.HasPrefix(op, "slow") {
			// Never DONE: the caller's deadline is what ends the wait.
			f.mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			f.mu.Lock()
			reply(compute.Operation{Name: op, Status: "RUNNING"})
			return
		}
		reply(compute.Operation{Name: op, Status: "DONE"})
	case zoneAt > 0 && r.Method == http.MethodPost:
		z := parts[zoneAt]
		f.inserts = append(f.inserts, z)
		switch f.insert[z] {
		case "conflict":
			w.WriteHeader(http.StatusConflict)
			reply(map[string]any{"error": map[string]any{"code": 409, "message": "already exists"}})
		case "slow":
			reply(compute.Operation{Name: "slow-" + z, Status: "RUNNING"})
		case "slow-landed":
			f.instances[z] = "PROVISIONING"
			reply(compute.Operation{Name: "slow-" + z, Status: "RUNNING"})
		default:
			f.instances[z] = "PROVISIONING"
			reply(compute.Operation{Name: "op-" + z, Status: "RUNNING"})
		}
	case zoneAt > 0 && r.Method == http.MethodDelete:
		z := parts[zoneAt]
		f.deletes = append(f.deletes, z)
		delete(f.instances, z)
		reply(compute.Operation{Name: "del-" + z, Status: "RUNNING"})
	case zoneAt > 0 && r.Method == http.MethodGet:
		st, ok := f.instances[parts[zoneAt]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			reply(map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})
			return
		}
		reply(compute.Instance{Name: "vm", Status: st})
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
	}
}

func newFakeClient(t *testing.T, f *fakeCompute) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	svc, err := compute.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return &Client{svc: svc, project: "p", zoneTimeout: 200 * time.Millisecond}
}

func createIn(c *Client, zones ...string) (string, error) {
	return c.Create(context.Background(), Config{Name: "vm", MachineType: "t", Zones: zones})
}

// An insert slower than the per-zone deadline can still make the machine, and
// that machine holds the name against every other zone, so it is used rather
// than deleted.
func TestCreateUsesASlowInsertThatLanded(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-a": "slow-landed", "z-b": "ok"}, instances: map[string]string{}}
	zone, err := createIn(newFakeClient(t, f), "z-a", "z-b")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-a" {
		t.Errorf("zone = %q, want the slow zone's machine", zone)
	}
	if len(f.deletes) != 0 || len(f.inserts) != 1 {
		t.Errorf("inserts %v, deletes %v; want one insert and no delete", f.inserts, f.deletes)
	}
}

// A slow insert that left nothing behind is cleaned up and the next zone tried.
func TestCreateMovesOnFromASlowInsertThatMadeNothing(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-a": "slow", "z-b": "ok"}, instances: map[string]string{}}
	zone, err := createIn(newFakeClient(t, f), "z-a", "z-b")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-b" {
		t.Errorf("zone = %q, want the next zone", zone)
	}
	if len(f.deletes) != 1 || f.deletes[0] != "z-a" {
		t.Errorf("deletes = %v, want the abandoned z-a cleaned up", f.deletes)
	}
}

// What failed in CI: the name is taken by an earlier zone's machine, so every
// later insert conflicts. The live machine is the one this create asked for.
func TestCreateAdoptsTheMachineAConflictNames(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-b": "conflict"}, instances: map[string]string{"z-a": "RUNNING"}}
	zone, err := createIn(newFakeClient(t, f), "z-b", "z-c")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-a" {
		t.Errorf("zone = %q, want the machine that holds the name", zone)
	}
}

// A machine on its way down is not a place to run a job: the conflict stays a
// failure for that zone, and the next is tried.
func TestCreateDoesNotAdoptADyingMachine(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-b": "conflict", "z-c": "ok"}, instances: map[string]string{"z-a": "STOPPING"}}
	zone, err := createIn(newFakeClient(t, f), "z-b", "z-c")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-c" {
		t.Errorf("zone = %q, want the next zone rather than the dying machine", zone)
	}
}
