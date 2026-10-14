package provider

import (
	"context"
	"io"
	"time"
)

// noopProvider erfüllt MailProvider und hält die Schnittstelle kompilierbar.
type noopProvider struct{}

func (noopProvider) Capabilities() Capabilities { return Capabilities{} }
func (noopProvider) ListMailboxes(context.Context) ([]Mailbox, error) {
	return nil, nil
}
func (noopProvider) Changes(context.Context, string, SyncState, int) (ChangeSet, error) {
	return ChangeSet{}, nil
}
func (noopProvider) FetchHeaders(context.Context, string, []UID) ([]Header, error) {
	return nil, nil
}
func (noopProvider) FetchRaw(context.Context, string, UID) (io.ReadCloser, error) {
	return nil, ErrUnsupported
}
func (noopProvider) SetFlags(context.Context, string, []UID, FlagPatch) error { return nil }
func (noopProvider) Move(context.Context, string, []UID, string) (map[UID]UID, error) {
	return nil, nil
}
func (noopProvider) Trash(context.Context, string, []UID) error { return nil }
func (noopProvider) Append(context.Context, string, []byte, Flags, time.Time) (UID, error) {
	return 0, nil
}
func (noopProvider) Idle(context.Context, string, chan<- struct{}) error { return ErrUnsupported }
func (noopProvider) Close() error                                        { return nil }
