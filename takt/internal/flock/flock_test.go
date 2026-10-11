// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package flock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/flock"
)

// shortWait bounds the wait on a lock the test holds itself.
const shortWait = 50 * time.Millisecond

func open(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestExclusiveWaitsOutAHolderThenTakesTheFreedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	holder, waiter := open(t, path), open(t, path)
	if err := flock.Exclusive(context.Background(), holder, shortWait); err != nil {
		t.Fatalf("free lock: %v", err)
	}
	if err := flock.Exclusive(context.Background(), waiter, shortWait); !errors.Is(err, flock.ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("held lock: got %v, want ErrTimeout wrapping DeadlineExceeded", err)
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := flock.Exclusive(context.Background(), waiter, shortWait); err != nil {
		t.Fatalf("released lock: %v", err)
	}
}
