package jobs

import "testing"

func TestDrainPriorityFirst(t *testing.T) {
	q := newMetaEnrichQueue()
	q.max = 10
	for i := 0; i < 8; i++ {
		q.items[reg("r", i)] = pendingMeta{id: reg("r", i)}
	}
	// 3 priority items
	for i := 0; i < 3; i++ {
		q.items[reg("p", i)] = pendingMeta{id: reg("p", i), priority: true}
	}

	drained := q.drain(5)
	if len(drained) != 5 {
		t.Fatalf("drained %d, want 5", len(drained))
	}
	prioCount := 0
	for _, e := range drained {
		if e.priority {
			prioCount++
		}
	}
	if prioCount != 3 {
		t.Errorf("priority drained = %d, want all 3 (priority must drain first)", prioCount)
	}
	if q.len() != 6 {
		t.Errorf("remaining = %d, want 6", q.len())
	}
}

func TestDrainPriorityOverflowGoesBack(t *testing.T) {
	q := newMetaEnrichQueue()
	q.max = 10
	for i := 0; i < 7; i++ {
		q.items[reg("p", i)] = pendingMeta{id: reg("p", i), priority: true}
	}
	for i := 0; i < 4; i++ {
		q.items[reg("r", i)] = pendingMeta{id: reg("r", i)}
	}
	drained := q.drain(5)
	if len(drained) != 5 {
		t.Fatalf("drained %d, want 5", len(drained))
	}
	for _, e := range drained {
		if !e.priority {
			t.Errorf("non-priority drained when priority backlog existed: %+v", e)
		}
	}
	if q.len() != 6 {
		t.Errorf("remaining = %d, want 6 (2 priority + 4 regular)", q.len())
	}
}

func reg(prefix string, i int) string {
	return prefix + string(rune('a'+i))
}