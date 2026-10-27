// bcrypt erzeugt den Passwort-Hash für das Dex-Testkonto (Spike P-AUTH).
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 10)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(h))
}
