package provider

import (
	"context"
	"io"
)

// noopProvider erfüllt MailProvider und hält die Schnittstelle kompilierbar.
type noopProvider struct{}

func (noopProvider) Capabilities() Capabilities { return Capabilities{} }
func (noopProvider) ListMailboxes(context.Context) ([]Mailbox, error) {
	return nil, nil
}
func (noopProvider) Changes(context.Context, string, SyncState, ChangesOptions) (ChangeSet, error) {
	return ChangeSet{}, nil
}
func (noopProvider) FetchHeaders(context.Context, string, uint32, []UID) ([]Header, error) {
	return nil, nil
}
func (noopProvider) FetchRaw(context.Context, string, uint32, []UID, func(UID, io.Reader) error) error {
	return ErrUnsupported
}
func (noopProvider) SetFlags(context.Context, string, []UID, FlagPatch) error { return nil }
func (noopProvider) Move(context.Context, string, []UID, string) (map[UID]UID, error) {
	return nil, nil
}
func (noopProvider) Trash(context.Context, string, []UID) error { return nil }
func (noopProvider) Append(context.Context, string, []byte, AppendOptions) (AppendResult, error) {
	return AppendResult{}, nil
}
func (noopProvider) Idle(context.Context, string, chan<- struct{}) error { return ErrUnsupported }
func (noopProvider) Close() error                                        { return nil }
