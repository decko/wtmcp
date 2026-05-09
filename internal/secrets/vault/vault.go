package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"
)

// Decrypt decrypts an Ansible Vault encrypted payload. The data must
// include the full vault file content (header + hex body). Returns
// the decrypted plaintext.
//
// Security: HMAC is verified before decryption (encrypt-then-MAC).
// Key material is zeroed after use (best-effort).
func Decrypt(data, password []byte) ([]byte, error) {
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("vault file too large: %d bytes (max %d)", len(data), maxFileSize)
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) != 2 {
		return nil, fmt.Errorf("invalid vault file: missing body")
	}

	if _, err := ParseHeader(lines[0]); err != nil {
		return nil, err
	}

	salt, expectedHMAC, ciphertext, err := splitPayload(lines[1])
	if err != nil {
		return nil, err
	}

	derived, err := pbkdf2.Key(sha256.New, string(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return nil, fmt.Errorf("vault decryption failed: key derivation: %w", err)
	}
	defer ZeroBytes(derived)

	aesKey := derived[aesKeyOffset : aesKeyOffset+aesKeyLen]
	hmacKey := derived[hmacKeyOffset : hmacKeyOffset+hmacKeyLen]
	iv := derived[ivOffset : ivOffset+ivLen]

	// HMAC covers ciphertext only (Ansible Vault format limitation:
	// salt and IV are not authenticated). In practice, modifying the
	// salt or IV changes the derived keys, which causes the HMAC to
	// fail — so they are indirectly authenticated. This is an accepted
	// risk for Ansible Vault format compatibility.
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)
	computedHMAC := mac.Sum(nil)

	if !hmac.Equal(computedHMAC, expectedHMAC) {
		return nil, fmt.Errorf("vault decryption failed: HMAC verification failed (wrong password?)")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("vault decryption failed: %w", err)
	}

	plaintext := make([]byte, len(ciphertext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(plaintext, ciphertext)

	unpadded, err := pkcs7Unpad(plaintext)
	if err != nil {
		ZeroBytes(plaintext)
		return nil, fmt.Errorf("vault decryption failed: %w", err)
	}

	if len(unpadded) != len(plaintext) {
		result := make([]byte, len(unpadded))
		copy(result, unpadded)
		ZeroBytes(plaintext)
		return result, nil
	}
	return plaintext, nil
}

// Encrypt encrypts data with Ansible Vault 1.1 format. The output
// is compatible with `ansible-vault decrypt`. Salt is generated via
// crypto/rand.
func Encrypt(data, password []byte) ([]byte, error) {
	return encryptWithHeader(data, password, "$ANSIBLE_VAULT;1.1;AES256")
}

// EncryptWithID encrypts data with Ansible Vault 1.2 format using
// the given vault ID label. The output is compatible with
// `ansible-vault decrypt --vault-id <label>@<password-source>`.
func EncryptWithID(data, password []byte, vaultID string) ([]byte, error) {
	if vaultID == "" {
		return nil, fmt.Errorf("vault ID label must not be empty (use Encrypt for 1.1 format)")
	}
	if len(vaultID) > maxVaultIDLen {
		return nil, fmt.Errorf("vault ID label too long: %d chars (max %d)", len(vaultID), maxVaultIDLen)
	}
	if !vaultIDPattern.MatchString(vaultID) {
		return nil, fmt.Errorf("invalid vault ID label: must match [a-zA-Z0-9_-]+")
	}
	header := fmt.Sprintf("$ANSIBLE_VAULT;1.2;AES256;%s", vaultID)
	return encryptWithHeader(data, password, header)
}

func encryptWithHeader(data, password []byte, header string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	padded := pkcs7Pad(data, aes.BlockSize)
	defer ZeroBytes(padded)

	derived, err := pbkdf2.Key(sha256.New, string(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return nil, fmt.Errorf("key derivation: %w", err)
	}
	defer ZeroBytes(derived)

	aesKey := derived[aesKeyOffset : aesKeyOffset+aesKeyLen]
	hmacKey := derived[hmacKeyOffset : hmacKeyOffset+hmacKeyLen]
	iv := derived[ivOffset : ivOffset+ivLen]

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	ciphertext := make([]byte, len(padded))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, padded)

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)
	hmacVal := mac.Sum(nil)

	body := formatPayload(salt, hmacVal, ciphertext)
	result := header + "\n" + body + "\n"
	return []byte(result), nil
}

// pkcs7Pad pads data to a multiple of blockSize using PKCS#7.
// Ansible Vault applies PKCS7 padding even though CTR mode does
// not require it (format oddity from the original Python impl).
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding) //nolint:gosec // padding is 1-16, safe for byte
	}
	return padded
}

// pkcs7Unpad removes PKCS#7 padding and validates it strictly.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("pkcs7: data is empty")
	}
	padding := int(data[len(data)-1])
	if padding < 1 || padding > 16 {
		return nil, fmt.Errorf("pkcs7: invalid padding value %d", padding)
	}
	if padding > len(data) {
		return nil, fmt.Errorf("pkcs7: padding %d exceeds data length %d", padding, len(data))
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, fmt.Errorf("pkcs7: invalid padding byte at position %d", i)
		}
	}
	return data[:len(data)-padding], nil
}

// ZeroBytes overwrites a byte slice with zeros. Best-effort memory
// hygiene — Go's GC may retain copies, but explicit zeroing reduces
// the exposure window.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}
