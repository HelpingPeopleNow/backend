package core

import "testing"

func TestDirectMessageReportTableName(t *testing.T) {
	if got := (DirectMessageReport{}).TableName(); got != "direct_message_reports" {
		t.Fatalf("TableName() = %q, want %q", got, "direct_message_reports")
	}
}
