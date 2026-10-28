package provider

import (
	"reflect"
	"strings"
	"testing"
)

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

// A03 E2, Akzeptanzkriterium 1 von S3: Die Schnittstelle hat keine
// Methode, die endgültig löscht (kein Expunge, kein Delete); Trash ist nur
// Verschieben in den Papierkorb.
func TestMailProviderHasNoExpungeOrDelete(t *testing.T) {
	typ := reflect.TypeFor[MailProvider]()
	for i := range typ.NumMethod() {
		name := strings.ToLower(typ.Method(i).Name)
		if strings.Contains(name, "expunge") || strings.Contains(name, "delete") || strings.Contains(name, "purge") {
			t.Errorf("MailProvider has %s; final deletion is not allowed (ADR-002, E2)", typ.Method(i).Name)
		}
	}
}
