//go:build race

package ops

// raceDetector: Die Tests laufen mit -race (make race, CI-Job race). Der
// Vertragstest prüft dann nur eine Stichprobe der Trockenlauf-Varianten
// (siehe TestContractEveryOperationRoleAndDryRun); die volle Matrix läuft in
// make check und im CI-Job test.
const raceDetector = true
