package httpmcp

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rejectLogEvery: Abgewiesene Anfragen stehen höchstens einmal je
// rejectLogEvery im Log, als eine Zeile mit Zählern (Nachbesserung T5,
// Punkt 9 l). Grund: postkauz.log hat keine Größengrenze und liegt im
// Datenverzeichnis des Dauer-Syncs; ohne Zusammenfassung füllte ein
// Teilnehmer im Tailnet mit abgewiesenen Anfragen die Platte.
const rejectLogEvery = 10 * time.Second

// rejectLog zählt abgewiesene Anfragen je Status und Grund (Schlüssel
// "429" oder "401 invalid_token") und schreibt sie zusammen.
type rejectLog struct {
	log *slog.Logger

	mu     sync.Mutex
	last   time.Time // letzte Sammelzeile
	since  time.Time // Beginn des laufenden Zeitraums
	counts map[string]int
}

// add zählt eine abgewiesene Anfrage und schreibt die Sammelzeile, wenn
// seit der letzten rejectLogEvery vergangen ist (die erste Abweisung nach
// einer ruhigen Zeit steht also sofort im Log).
func (l *rejectLog) add(now time.Time, status int, reason string) {
	key := strconv.Itoa(status)
	if reason != "" {
		key += " " + reason
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	if len(l.counts) == 0 {
		l.since = now
	}
	l.counts[key]++
	l.flushLocked(now, false)
}

// tick schreibt ausstehende Zähler, wenn der Zeitraum um ist (bei jeder
// Anfrage, auch einer angenommenen).
func (l *rejectLog) tick(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked(now, false)
}

// flush schreibt ausstehende Zähler sofort (Ende von Serve).
func (l *rejectLog) flush(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked(now, true)
}

func (l *rejectLog) flushLocked(now time.Time, force bool) {
	if len(l.counts) == 0 {
		return
	}
	if !force && !l.last.IsZero() && now.Sub(l.last) < rejectLogEvery && !now.Before(l.last) {
		return
	}
	keys := make([]string, 0, len(l.counts))
	total := 0
	for k, n := range l.counts {
		keys = append(keys, k)
		total += n
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + strconv.Itoa(l.counts[k])
	}
	l.log.Warn("http requests rejected", "count", total, "by_status", strings.Join(parts, ", "),
		"seconds", int64(now.Sub(l.since).Seconds()))
	l.counts = map[string]int{}
	l.last = now
}
