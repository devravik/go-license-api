package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", ".keys/signing_key.b64", "Output file path for base64-encoded Ed25519 private key")
	flag.Parse()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate keypair: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir: %v\n", err)
		os.Exit(1)
	}

	encoded := base64.StdEncoding.EncodeToString(priv)
	if err := os.WriteFile(*out, []byte(encoded+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write key file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Signing key generated: %s\n", *out)
	fmt.Println("Next steps:")
	fmt.Printf("1) Set SIGNING_KEY_PATH in .env:\n   SIGNING_KEY_PATH=%s\n", *out)
	fmt.Println("2) Restart the server.")
}

