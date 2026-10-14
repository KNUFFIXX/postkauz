package provider

import "testing"

func TestSyncStrategy(t *testing.T) {
	tests := []struct {
		name string
		c    Capabilities
		want Strategy
	}{
		{"nothing known", Capabilities{}, StrategyUIDScan},
		{"condstore only", Capabilities{CondStore: true}, StrategyCondStore},
		{"qresync needs condstore", Capabilities{QResync: true}, StrategyUIDScan},
		{"both", Capabilities{CondStore: true, QResync: true}, StrategyQResync},
		{"idle does not matter", Capabilities{Idle: true}, StrategyUIDScan},
	}
	for _, tc := range tests {
		if got := tc.c.SyncStrategy(); got != tc.want {
			t.Errorf("%s: SyncStrategy = %s, want %s", tc.name, got, tc.want)
		}
	}
}

// Kompilierzeit-Prüfung: Die Schnittstelle bleibt implementierbar. Sie enthält
// absichtlich kein Expunge; wer eines ergänzt, ändert bewusst ADR-002.
func TestNoopProviderSatisfiesInterface(t *testing.T) {
	var _ MailProvider = (*noopProvider)(nil)
}
