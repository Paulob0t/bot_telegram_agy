package sessions

import (
	"context"
	"testing"
	"time"
)

func TestManagerPruneExpiredAndKill(t *testing.T) {
	mgr := NewManager(time.Hour)

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	mgr.Upsert("s1", "Stover", "Tablas", cancel1)
	mgr.Upsert("s2", "Agente", "Relay", cancel2)

	// Simulate s1 idle for 61 minutes
	mgr.mu.Lock()
	mgr.sessions["s1"].LastActivity = time.Now().Add(-61 * time.Minute)
	mgr.mu.Unlock()

	pruned := mgr.PruneExpired(time.Now())
	if len(pruned) != 1 || pruned[0] != "s1" {
		t.Fatalf("expected s1 pruned, got %v", pruned)
	}
	if ctx1.Err() == nil {
		t.Fatalf("expected s1 context cancelled")
	}
	if ctx2.Err() != nil {
		t.Fatalf("expected s2 context still active")
	}

	if !mgr.Kill("s2") {
		t.Fatalf("expected Kill(s2) true")
	}
	if ctx2.Err() == nil {
		t.Fatalf("expected s2 context cancelled after Kill")
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("expected 0 sessions remaining")
	}
}
