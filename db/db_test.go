package db

import "testing"

func TestInitDB_EmptyDatabaseURL(t *testing.T) {
	err := InitDB("")
	if err == nil {
		t.Fatal("expected error for empty databaseURL, got nil")
	}
}
