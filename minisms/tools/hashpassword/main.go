package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const minLen = 8

func main() {
	stdinFD := int(syscall.Stdin)
	if term.IsTerminal(stdinFD) {
		runInteractive()
		return
	}
	runPipe()
}

func runInteractive() {
	fmt.Println("MiniSMS Admin Password Hasher")
	fmt.Println("──────────────────────────────")

	fmt.Print("Enter password: ")
	pass1, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: failed to read password")
		os.Exit(1)
	}

	fmt.Print("Confirm password: ")
	pass2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: failed to read password confirmation")
		os.Exit(1)
	}

	p1 := string(pass1)
	p2 := string(pass2)
	if p1 != p2 {
		fmt.Fprintln(os.Stderr, "Error: passwords do not match")
		os.Exit(1)
	}
	if err := validatePassword(strings.TrimSpace(p1)); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(p1), 12)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: failed to generate bcrypt hash")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("ADMIN_PASSWORD_HASH=%s\n", string(hash))
	fmt.Println()
	fmt.Println("Add this line to your /etc/minisms/minisms.env file.")
}

func runPipe() {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		fmt.Fprintln(os.Stderr, "Error: password must not be empty")
		os.Exit(1)
	}
	pw := strings.TrimSpace(line)
	if err := validatePassword(pw); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: failed to generate bcrypt hash")
		os.Exit(1)
	}
	fmt.Println(string(hash))
}

func validatePassword(pw string) error {
	if pw == "" {
		return fmt.Errorf("Error: password must not be empty")
	}
	if len([]rune(pw)) < minLen {
		return fmt.Errorf("Error: minimum 8 characters")
	}
	return nil
}

