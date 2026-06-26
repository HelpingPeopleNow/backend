package contextkeys

import (
	"context"
	"testing"
)

func TestGetSetUserID(t *testing.T) {
	ctx := context.Background()
	got := GetUserID(ctx)
	if got != "" {
		t.Fatalf("expected empty string for missing key, got %q", got)
	}

	ctx = SetUserID(ctx, "user-42")
	if got := GetUserID(ctx); got != "user-42" {
		t.Fatalf("expected user-42, got %q", got)
	}
}

func TestGetSetIsAdmin(t *testing.T) {
	ctx := context.Background()
	if got := GetIsAdmin(ctx); got != false {
		t.Fatalf("expected false for missing key, got %v", got)
	}

	ctx = SetIsAdmin(ctx, true)
	if !GetIsAdmin(ctx) {
		t.Fatal("expected true after SetIsAdmin(true)")
	}

	ctx = SetIsAdmin(ctx, false)
	if GetIsAdmin(ctx) {
		t.Fatal("expected false after SetIsAdmin(false)")
	}
}

func TestGetUserIDWrongType(t *testing.T) {
	type wrongType string
	ctx := context.WithValue(context.Background(), UserIDKey, wrongType("not-a-string"))
	if got := GetUserID(ctx); got != "" {
		t.Fatalf("expected empty string for wrong type, got %q", got)
	}
}

func TestGetIsAdminWrongType(t *testing.T) {
	type wrongType string
	ctx := context.WithValue(context.Background(), IsAdminKey, wrongType("not-a-bool"))
	if got := GetIsAdmin(ctx); got != false {
		t.Fatalf("expected false for wrong type, got %v", got)
	}
}
