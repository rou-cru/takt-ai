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

// Package flock takes exclusive advisory file locks with a bounded wait.
package flock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// PollInterval is how often a waiting Exclusive retries the lock.
const PollInterval = 20 * time.Millisecond

// ErrTimeout is returned when the wait ended before the lock was free; it
// wraps the context error that ended it.
var ErrTimeout = errors.New("lock wait ended")

// Exclusive takes the exclusive advisory lock on f, retrying until ctx ends or
// wait elapses. It never closes f.
func Exclusive(ctx context.Context, f *os.File, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return err
		}
		timer := time.NewTimer(PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
