// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package gce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// fakeCompute is the slice of the compute API Create uses. Each zone's insert
// either succeeds, conflicts, runs out of capacity, or never finishes; a slow one
// can leave a machine behind, as GCE did when it took longer than the per-zone
// deadline.
type fakeCompute struct {
	mu sync.Mutex
	// "<type>/<zone>" or just "<zone>" -> "ok" | "conflict" | "exhausted" | "slow" |
	// "late" | "late-failed"; the type-qualified key wins.
	insert    map[string]string
	instances map[string]string // zone -> status
	inserts   []string          // "<type> in <zone>", in order
	deletes   []string
	// lateDone is when a late insert's operation finishes: past the per-zone
	// deadline, inside the second one a landed machine gets.
	lateDone map[string]time.Time
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
		if done, ok := f.lateDone[op]; ok {
			if time.Now().Before(done) {
				f.mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				f.mu.Lock()
				reply(compute.Operation{Name: op, Status: "RUNNING"})
				return
			}
			if strings.HasPrefix(op, "late-failed") {
				// GCE takes back a machine whose insert failed.
				delete(f.instances, parts[zoneAt])
				reply(compute.Operation{Name: op, Status: "DONE", Error: &compute.OperationError{
					Errors: []*compute.OperationErrorErrors{{Code: "ZONE_RESOURCE_POOL_EXHAUSTED", Message: "no capacity"}}}})
				return
			}
			reply(compute.Operation{Name: op, Status: "DONE"})
			return
		}
		if strings.HasPrefix(op, "exhausted") {
			reply(compute.Operation{Name: op, Status: "DONE", Error: &compute.OperationError{
				Errors: []*compute.OperationErrorErrors{{Code: "ZONE_RESOURCE_POOL_EXHAUSTED", Message: "no capacity"}}}})
			return
		}
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
		var body compute.Instance
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		machineType := body.MachineType[strings.LastIndex(body.MachineType, "/")+1:]
		f.inserts = append(f.inserts, machineType+" in "+z)
		behaviour, ok := f.insert[machineType+"/"+z]
		if !ok {
			behaviour = f.insert[z]
		}
		switch behaviour {
		case "conflict":
			w.WriteHeader(http.StatusConflict)
			reply(map[string]any{"error": map[string]any{"code": 409, "message": "already exists"}})
		case "exhausted":
			reply(compute.Operation{Name: "exhausted-" + z, Status: "RUNNING"})
		case "slow":
			reply(compute.Operation{Name: "slow-" + z, Status: "RUNNING"})
		case "late", "late-failed":
			f.instances[z] = "PROVISIONING"
			op := behaviour + "-" + z
			if f.lateDone == nil {
				f.lateDone = map[string]time.Time{}
			}
			f.lateDone[op] = time.Now().Add(600 * time.Millisecond)
			reply(compute.Operation{Name: op, Status: "RUNNING"})
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
	return &Client{svc: svc, project: "p", zoneTimeout: 400 * time.Millisecond}
}

func createIn(c *Client, zones ...string) (string, error) {
	return c.Create(context.Background(), Config{Name: "vm", MachineType: "t", Zones: zones})
}

// An insert slower than the per-zone deadline whose machine already exists holds
// the name against every other zone, so it is waited out rather than abandoned.
func TestCreateWaitsOutASlowInsertThatLanded(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-a": "late", "z-b": "ok"}, instances: map[string]string{}}
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

// A slow insert can still fail after its machine appeared; that is not a machine
// to run on, so it is cleaned up and the next zone tried.
func TestCreateMovesOnFromASlowInsertThatFailedLate(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-a": "late-failed", "z-b": "ok"}, instances: map[string]string{}}
	zone, err := createIn(newFakeClient(t, f), "z-a", "z-b")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-b" {
		t.Errorf("zone = %q, want the next zone", zone)
	}
	if len(f.deletes) != 1 || f.deletes[0] != "z-a" {
		t.Errorf("deletes = %v, want the failed z-a cleaned up", f.deletes)
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

// An insert that never finishes, with its machine already there, gets one more
// per-zone deadline and no more: waiting it out on the overall deadline would
// leave every remaining fallback unattempted.
func TestCreateGivesUpOnAWedgedInsertAndFallsBack(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-a": "slow", "z-b": "ok"}, instances: map[string]string{"z-a": "PROVISIONING"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	zone, err := newFakeClient(t, f).Create(ctx, Config{Name: "vm", MachineTypes: []string{"t1", "t2"}, Zones: []string{"z-a", "z-b"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-b" {
		t.Errorf("zone = %q, want the next zone", zone)
	}
	if want := []string{"t1 in z-a", "t1 in z-b"}; !slices.Equal(f.inserts, want) {
		t.Errorf("inserts = %v, want %v", f.inserts, want)
	}
	if len(f.deletes) != 1 || f.deletes[0] != "z-a" {
		t.Errorf("deletes = %v, want the wedged z-a cleaned up", f.deletes)
	}
}

// Capacity runs out by machine type as well as by zone: the first type is tried in
// every zone before the next, and the first success ends the search.
func TestCreateFallsBackToTheNextMachineType(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{
		"t1/z-a": "exhausted", "t1/z-b": "exhausted",
		"t2/z-a": "exhausted", "t2/z-b": "ok",
	}, instances: map[string]string{}}
	zone, err := newFakeClient(t, f).Create(context.Background(), Config{Name: "vm", MachineTypes: []string{"t1", "t2", "t3"}, Zones: []string{"z-a", "z-b"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if zone != "z-b" {
		t.Errorf("zone = %q, want z-b", zone)
	}
	if want := []string{"t1 in z-a", "t1 in z-b", "t2 in z-a", "t2 in z-b"}; !slices.Equal(f.inserts, want) {
		t.Errorf("inserts = %v, want %v and nothing after the success", f.inserts, want)
	}
}

// A live machine with this name that this create did not make, such as one a
// rerun's earlier pod left behind, is never taken as this job's.
func TestCreateDoesNotAdoptAMachineItDidNotMake(t *testing.T) {
	f := &fakeCompute{insert: map[string]string{"z-b": "conflict", "z-c": "conflict"}, instances: map[string]string{"z-a": "RUNNING"}}
	if zone, err := createIn(newFakeClient(t, f), "z-b", "z-c"); err == nil {
		t.Errorf("Create = %q, want a failure rather than someone else's machine", zone)
	}
	if len(f.deletes) != 0 {
		t.Errorf("deletes = %v, want the other machine left alone", f.deletes)
	}
}

// Billing reports a disk under its own labels, so an unlabelled boot disk is
// cost no run is charged for.
func TestBootDiskCarriesTheInstanceLabels(t *testing.T) {
	labels := map[string]string{"ci-provider": "argoci", "ci-workflow-id": "wf"}
	inst := (&Client{project: "p"}).instanceSpec("z-a", "t", Config{Name: "vm", DiskType: "pd-ssd", Labels: labels})
	got := inst.Disks[0].InitializeParams.Labels
	if len(got) != len(labels) || got["ci-workflow-id"] != "wf" {
		t.Errorf("boot disk labels = %v, want %v", got, labels)
	}
}
