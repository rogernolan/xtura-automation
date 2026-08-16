//go:build !linux

package switchbot

import "context"

// scanLoop is unavailable off-Linux; the Run loop surfaces ErrUnsupported and
// backs off without exiting.
func (a *Adapter) scanLoop(ctx context.Context, device string) error {
	return ErrUnsupported
}
