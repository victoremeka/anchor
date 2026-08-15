package idempotency

import "testing"

func TestKey_Deterministic(t *testing.T) {
	a := Key("task-1", "send_email", "")
	b := Key("task-1", "send_email", "")
	if a != b {
		t.Fatalf("same inputs produced different keys: %q vs %q", a, b)
	}
}

func TestKey_CallKeyDisambiguates(t *testing.T) {
	a := Key("task-1", "send_email", "recipient-a")
	b := Key("task-1", "send_email", "recipient-b")
	if a == b {
		t.Fatalf("different call_keys collided on the same key: %q", a)
	}
}

func TestKey_NoSeparatorCollision(t *testing.T) {
	a := Key("ab", "c", "")
	b := Key("a", "bc", "")
	if a == b {
		t.Fatalf("task_id/tool_name boundary collided: %q", a)
	}
}

func TestKey_TaskAndToolBothDisambiguate(t *testing.T) {
	a := Key("task-1", "send_email", "x")
	b := Key("task-2", "send_email", "x")
	c := Key("task-1", "refund", "x")
	if a == b || a == c || b == c {
		t.Fatalf("task_id or tool_name failed to disambiguate: a=%q b=%q c=%q", a, b, c)
	}
}
