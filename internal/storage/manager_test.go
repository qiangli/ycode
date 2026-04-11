package storage

import (
	"context"
	"testing"
	"time"
)

// mockKV implements KVStore for testing.
type mockKV struct{}

func (m *mockKV) Get(_, _ string) ([]byte, error)                      { return nil, nil }
func (m *mockKV) Put(_, _ string, _ []byte) error                      { return nil }
func (m *mockKV) Delete(_, _ string) error                             { return nil }
func (m *mockKV) List(_ string) ([]string, error)                      { return nil, nil }
func (m *mockKV) ForEach(_ string, _ func(string, []byte) error) error { return nil }
func (m *mockKV) Close() error                                         { return nil }

// mockSQL implements SQLStore for testing.
type mockSQL struct{}

func (m *mockSQL) Exec(_ context.Context, _ string, _ ...any) (Result, error) { return nil, nil }
func (m *mockSQL) QueryRow(_ context.Context, _ string, _ ...any) Row         { return nil }
func (m *mockSQL) Query(_ context.Context, _ string, _ ...any) (Rows, error)  { return nil, nil }
func (m *mockSQL) Tx(_ context.Context, _ func(SQLStore) error) error         { return nil }
func (m *mockSQL) Migrate(_ context.Context) error                            { return nil }
func (m *mockSQL) Close() error                                               { return nil }

func TestManagerPhase1(t *testing.T) {
	ctx := context.Background()

	mgr, err := NewManager(ctx, Config{
		DataDir: t.TempDir(),
		KVFactory: func(_ context.Context) (KVStore, error) {
			return &mockKV{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	if mgr.KV() == nil {
		t.Error("KV should be ready after NewManager")
	}

	status := mgr.Status()
	if !status.KV.Ready {
		t.Error("KV status should be ready")
	}
	if status.KV.Phase != Phase1 {
		t.Errorf("KV phase = %d, want %d", status.KV.Phase, Phase1)
	}
}

func TestManagerPhase2Background(t *testing.T) {
	ctx := context.Background()

	mgr, err := NewManager(ctx, Config{
		DataDir: t.TempDir(),
		KVFactory: func(_ context.Context) (KVStore, error) {
			return &mockKV{}, nil
		},
		SQLFactory: func(_ context.Context) (SQLStore, error) {
			return &mockSQL{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	// SQL not ready yet.
	select {
	case <-mgr.SQLReady():
		t.Error("SQL should not be ready before InitSQL")
	default:
		// expected
	}

	// Start background init.
	mgr.StartBackground(ctx)

	// Wait for SQL.
	select {
	case <-mgr.SQLReady():
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("SQL init timed out")
	}

	sql := mgr.SQL(ctx)
	if sql == nil {
		t.Error("SQL should be ready after background init")
	}

	status := mgr.Status()
	if !status.SQL.Ready {
		t.Error("SQL status should be ready")
	}
}

func TestManagerClose(t *testing.T) {
	ctx := context.Background()

	mgr, err := NewManager(ctx, Config{
		DataDir: t.TempDir(),
		KVFactory: func(_ context.Context) (KVStore, error) {
			return &mockKV{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestManagerNoFactories(t *testing.T) {
	ctx := context.Background()

	mgr, err := NewManager(ctx, Config{
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	// All backends should be nil but not panic.
	if mgr.KV() != nil {
		t.Error("KV should be nil without factory")
	}

	mgr.StartBackground(ctx)

	// SQL/Vector/Search should resolve to nil without blocking forever.
	shortCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	if mgr.SQL(shortCtx) != nil {
		t.Error("SQL should be nil without factory")
	}
	if mgr.Vector(shortCtx) != nil {
		t.Error("Vector should be nil without factory")
	}
	if mgr.Search(shortCtx) != nil {
		t.Error("Search should be nil without factory")
	}
}
