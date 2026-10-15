// Package thread ordnet Nachrichten zu Threads (Gesprächsfäden).
//
// Stand S1.2: Zuordnung über Message-ID, In-Reply-To und References per
// Union-Find (Spike: 60/60 Threads exakt, nichts vermischt). Fehlende
// Zwischenglieder verbinden trotzdem, weil auch referenzierte, nicht
// vorhandene IDs Knoten sind. Der Rückfall über den normalisierten Betreff
// im Zeitfenster (docs/datenmodell.md, Tabelle threads) folgt in S1.6.
package thread

// Node ist eine Nachricht aus Sicht des Threadings.
type Node struct {
	// MessageID ohne spitze Klammern; leer, wenn die Nachricht keine hat
	// (dann bildet sie einen eigenen Thread, außer sie verweist auf andere).
	MessageID  string
	InReplyTo  []string
	References []string
}

type unionFind struct {
	parent map[string]string
}

func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		return x
	}
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]] // Pfadhalbierung
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	// deterministisch: kleinere ID wird Wurzel
	if rb < ra {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
}

// Group liefert je Eingabe eine Thread-Nummer (0, 1, 2, … in der
// Reihenfolge des ersten Auftretens). Gleiche Nummer = gleicher Thread.
func Group(nodes []Node) []int {
	u := &unionFind{parent: map[string]string{}}
	keys := make([]string, len(nodes))
	for i, n := range nodes {
		key := "id:" + n.MessageID
		if n.MessageID == "" {
			// eigener Schlüssel; Präfix verhindert Kollision mit echten IDs
			key = "anon:" + itoa(i)
		}
		keys[i] = key
		u.find(key)
		for _, r := range n.References {
			if r != "" && r != n.MessageID {
				u.union(key, "id:"+r)
			}
		}
		for _, r := range n.InReplyTo {
			if r != "" && r != n.MessageID {
				u.union(key, "id:"+r)
			}
		}
	}
	out := make([]int, len(nodes))
	numbers := map[string]int{}
	for i, k := range keys {
		root := u.find(k)
		n, ok := numbers[root]
		if !ok {
			n = len(numbers)
			numbers[root] = n
		}
		out[i] = n
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
