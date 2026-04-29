// Package vault implements Ansible Vault 1.1/1.2 encryption and
// decryption in pure Go. It handles the $ANSIBLE_VAULT format with
// AES-256-CTR encryption, PBKDF2-HMAC-SHA256 key derivation, and
// HMAC-SHA256 authentication.
package vault

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	// magic is the header prefix for Ansible Vault encrypted files.
	magic = "$ANSIBLE_VAULT;"

	// maxFileSize is the maximum allowed vault file size (1 MB).
	maxFileSize = 1 << 20

	// maxVaultIDLen is the maximum allowed vault ID label length.
	maxVaultIDLen = 64

	// supportedCipher is the only cipher supported by Ansible Vault.
	supportedCipher = "AES256"

	// pbkdf2Iterations is the hardcoded iteration count for
	// Ansible Vault's PBKDF2 key derivation.
	pbkdf2Iterations = 10000

	// pbkdf2KeyLen is the total derived key length (80 bytes):
	// 32 bytes AES key + 32 bytes HMAC key + 16 bytes IV.
	pbkdf2KeyLen = 80

	saltLen       = 32
	hmacLen       = 32
	aesKeyOffset  = 0
	aesKeyLen     = 32
	hmacKeyOffset = 32
	hmacKeyLen    = 32
	ivOffset      = 64
	ivLen         = 16
)

// vaultIDPattern validates vault ID labels from untrusted file headers.
var vaultIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Header holds the parsed fields from an Ansible Vault header line.
type Header struct {
	Version string // "1.1" or "1.2"
	Cipher  string // "AES256"
	VaultID string // "" for 1.1 or unlabeled 1.2, label for labeled 1.2
}

// IsAnsibleVault reports whether data begins with the Ansible Vault
// magic header prefix.
func IsAnsibleVault(data []byte) bool {
	return len(data) >= len(magic) && string(data[:len(magic)]) == magic
}

// ParseHeader parses an Ansible Vault header line and validates its
// fields. Returns an error for unsupported versions, ciphers, or
// invalid vault ID labels.
func ParseHeader(headerLine string) (Header, error) {
	headerLine = strings.TrimRight(headerLine, "\r\n")
	parts := strings.Split(headerLine, ";")

	if len(parts) < 3 || len(parts) > 4 {
		return Header{}, fmt.Errorf("invalid vault header: expected 3-4 fields, got %d", len(parts))
	}
	if parts[0] != "$ANSIBLE_VAULT" {
		return Header{}, fmt.Errorf("invalid vault header: missing $ANSIBLE_VAULT prefix")
	}

	version := parts[1]
	if version != "1.1" && version != "1.2" {
		return Header{}, fmt.Errorf("unsupported vault format: %s", version)
	}

	cipher := parts[2]
	if cipher != supportedCipher {
		return Header{}, fmt.Errorf("unsupported vault cipher: %s", cipher)
	}

	if version == "1.1" && len(parts) == 4 {
		return Header{}, fmt.Errorf("vault 1.1 header must not have a vault ID label (use 1.2 format)")
	}

	var vaultID string
	if len(parts) == 4 {
		vaultID = parts[3]
		if vaultID == "" {
			return Header{}, fmt.Errorf("vault header has empty vault ID label")
		}
		if len(vaultID) > maxVaultIDLen {
			return Header{}, fmt.Errorf("vault ID label too long: %d chars (max %d)", len(vaultID), maxVaultIDLen)
		}
		if !vaultIDPattern.MatchString(vaultID) {
			return Header{}, fmt.Errorf("invalid vault ID label: must match [a-zA-Z0-9_-]+")
		}
	}

	return Header{
		Version: version,
		Cipher:  cipher,
		VaultID: vaultID,
	}, nil
}

// splitPayload parses the hex-encoded vault body into its three
// components: salt, HMAC, and ciphertext. The body lines are joined,
// hex-decoded, then split on newlines into exactly three hex-encoded
// parts which are individually decoded.
func splitPayload(body string) (salt, hmacVal, ciphertext []byte, err error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSpace(body)

	outerHex := strings.ReplaceAll(body, "\n", "")
	inner, err := hex.DecodeString(outerHex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: outer hex decode failed")
	}

	parts := strings.SplitN(string(inner), "\n", 4)
	if len(parts) != 3 {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: expected 3 parts, got %d", len(parts))
	}

	salt, err = hex.DecodeString(parts[0])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: salt hex decode failed")
	}
	if len(salt) != saltLen {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: salt is %d bytes, expected %d", len(salt), saltLen)
	}

	hmacVal, err = hex.DecodeString(parts[1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: HMAC hex decode failed")
	}
	if len(hmacVal) != hmacLen {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: HMAC is %d bytes, expected %d", len(hmacVal), hmacLen)
	}

	ciphertext, err = hex.DecodeString(parts[2])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: ciphertext hex decode failed")
	}
	if len(ciphertext) == 0 {
		return nil, nil, nil, fmt.Errorf("invalid vault file format: ciphertext is empty")
	}

	return salt, hmacVal, ciphertext, nil
}

// formatPayload encodes salt, HMAC, and ciphertext into the Ansible
// Vault hex body format: each component is hex-encoded, joined by
// newlines, then the whole thing is hex-encoded and wrapped at 80
// characters per line.
func formatPayload(salt, hmacVal, ciphertext []byte) string {
	inner := strings.Join([]string{
		hex.EncodeToString(salt),
		hex.EncodeToString(hmacVal),
		hex.EncodeToString(ciphertext),
	}, "\n")

	outer := hex.EncodeToString([]byte(inner))

	var lines []string
	for i := 0; i < len(outer); i += 80 {
		end := min(i+80, len(outer))
		lines = append(lines, outer[i:end])
	}
	return strings.Join(lines, "\n")
}
