package metrics

import (
	"strings"
	"testing"
)

func TestSetReembedEnabledTrue(t *testing.T) {
	SetReembedEnabled(true)
	out := Render()
	if !strings.Contains(out, "reembed_enabled") {
		t.Fatal("expected reembed_enabled in render output")
	}
	if !strings.Contains(out, `dummy="default"`) {
		t.Fatal("expected dummy label in output")
	}
	// Value should be 1
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "reembed_enabled{") && !strings.HasPrefix(line, "#") {
			if !strings.Contains(line, " 1") {
				t.Fatalf("expected reembed_enabled=1, got: %s", line)
			}
			return
		}
	}
	t.Fatal("reembed_enabled metric line not found")
}

func TestSetReembedEnabledFalse(t *testing.T) {
	SetReembedEnabled(false)
	out := Render()
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "reembed_enabled{") && !strings.HasPrefix(line, "#") {
			if !strings.Contains(line, " 0") {
				t.Fatalf("expected reembed_enabled=0, got: %s", line)
			}
			return
		}
	}
	t.Fatal("reembed_enabled metric line not found")
}

func TestIncrReembedSkipped(t *testing.T) {
	// Reset by setting fresh — in real code this is additive, but we just
	// verify it doesn't panic and renders.
	IncrReembedSkipped("kill_switch")
	IncrReembedSkipped("no_profile")
	IncrReembedSkipped("no_fields")
	IncrReembedSkipped("no_change")
	out := Render()
	if !strings.Contains(out, "reembed_skipped_total") {
		t.Fatal("expected reembed_skipped_total in render output")
	}
}

func TestIncrReembedCompleted(t *testing.T) {
	IncrReembedCompleted("ok")
	IncrReembedCompleted("embed_err")
	IncrReembedCompleted("upsert_err")
	out := Render()
	if !strings.Contains(out, "reembed_completed_total") {
		t.Fatal("expected reembed_completed_total in render output")
	}
}

func TestRenderIncludesHelpAndType(t *testing.T) {
	out := Render()
	if !strings.Contains(out, "# HELP reembed_enabled") {
		t.Fatal("expected HELP line for reembed_enabled")
	}
	if !strings.Contains(out, "# TYPE reembed_enabled gauge") {
		t.Fatal("expected TYPE line for reembed_enabled")
	}
	if !strings.Contains(out, "# TYPE reembed_skipped_total counter") {
		t.Fatal("expected TYPE line for reembed_skipped_total")
	}
	if !strings.Contains(out, "# TYPE reembed_completed_total counter") {
		t.Fatal("expected TYPE line for reembed_completed_total")
	}
}
