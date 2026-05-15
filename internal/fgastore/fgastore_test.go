package fgastore

import (
	"context"
	"testing"
)

func TestNewInMemory_Smoke(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatalf("NewInMemory: %v", err)
	}
	defer func() { _ = f.Close() }()
}

func TestCheck_ReturnsTrueForExistingTuple(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Write(ctx, "user:alice", "can_read", "record:doc1"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ok, err := f.Check(ctx, "user:alice", "can_read", "record:doc1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !ok {
		t.Error("expected Check to return true for written tuple")
	}
}

func TestCheck_ReturnsFalseForNonExistentTuple(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	ok, err := f.Check(ctx, "user:alice", "can_read", "record:doc1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ok {
		t.Error("expected Check to return false for non-existent tuple")
	}
}

func TestCheck_ReturnsFalseAfterDelete(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Write(ctx, "user:alice", "can_read", "record:doc1"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Delete(ctx, "user:alice", "can_read", "record:doc1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	ok, err := f.Check(ctx, "user:alice", "can_read", "record:doc1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ok {
		t.Error("expected Check to return false after Delete")
	}
}

func TestCheck_DifferentRelation(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Write(ctx, "user:alice", "can_read", "record:doc1"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ok, err := f.Check(ctx, "user:alice", "owner", "record:doc1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ok {
		t.Error("expected Check to return false for different relation")
	}
}

func TestCheck_DifferentUser(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Write(ctx, "user:alice", "can_read", "record:doc1"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ok, err := f.Check(ctx, "user:bob", "can_read", "record:doc1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ok {
		t.Error("expected Check to return false for different user")
	}
}
