package main

import (
	"bytes"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/LeGambiArt/wtmcp/internal/config"
	"github.com/LeGambiArt/wtmcp/internal/secrets/vault"
)

var (
	vaultPasswordFile string
	vaultID           string
	vaultAskPass      bool
	vaultCheckOnly    bool
)

var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "Encrypt and decrypt Ansible Vault files",
}

var vaultEncryptCmd = &cobra.Command{
	Use:   "encrypt <file> [file...]",
	Short: "Encrypt files with Ansible Vault",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runVaultEncrypt,
}

var vaultDecryptCmd = &cobra.Command{
	Use:   "decrypt <file> [file...]",
	Short: "Decrypt Ansible Vault files",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runVaultDecrypt,
}

var vaultViewCmd = &cobra.Command{
	Use:   "view <file>",
	Short: "View decrypted content without modifying the file",
	Args:  cobra.ExactArgs(1),
	RunE:  runVaultView,
}

func init() {
	vaultCmd.PersistentFlags().StringVar(&vaultPasswordFile, "vault-password-file", "",
		"Read vault password from file")
	vaultCmd.PersistentFlags().BoolVar(&vaultAskPass, "ask-vault-pass", false,
		"Prompt for vault password")

	vaultEncryptCmd.Flags().StringVar(&vaultID, "vault-id", "",
		"Vault ID label for 1.2 format")

	vaultDecryptCmd.Flags().BoolVar(&vaultCheckOnly, "check", false,
		"Verify decryption without writing")

	vaultCmd.AddCommand(vaultEncryptCmd, vaultDecryptCmd, vaultViewCmd)
}

func runVaultEncrypt(_ *cobra.Command, args []string) error {
	password, err := resolveVaultCLIPassword(true)
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(password)

	for _, path := range args {
		data, err := os.ReadFile(path) //nolint:gosec // user-specified path
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if vault.IsAnsibleVault(data) {
			return fmt.Errorf("%s is already encrypted", path)
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		var encrypted []byte
		if vaultID != "" {
			encrypted, err = vault.EncryptWithID(data, password, vaultID)
		} else {
			encrypted, err = vault.Encrypt(data, password)
		}
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", path, err)
		}

		if err := atomicWriteFile(path, encrypted, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("Encryption successful: %s\n", path)
	}
	return nil
}

func runVaultDecrypt(_ *cobra.Command, args []string) error {
	password, err := resolveVaultCLIPassword(false)
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(password)

	for _, path := range args {
		data, err := os.ReadFile(path) //nolint:gosec // user-specified path
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if !vault.IsAnsibleVault(data) {
			return fmt.Errorf("%s is not an Ansible Vault encrypted file", path)
		}

		plaintext, err := vault.Decrypt(data, password)
		if err != nil {
			return fmt.Errorf("decrypt %s: %w", path, err)
		}

		if vaultCheckOnly {
			vault.ZeroBytes(plaintext)
			fmt.Printf("Decryption successful: %s\n", path)
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			vault.ZeroBytes(plaintext)
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if err := atomicWriteFile(path, plaintext, info.Mode().Perm()); err != nil {
			vault.ZeroBytes(plaintext)
			return fmt.Errorf("write %s: %w", path, err)
		}
		vault.ZeroBytes(plaintext)
		fmt.Printf("Decryption successful: %s\n", path)
	}
	return nil
}

func runVaultView(_ *cobra.Command, args []string) error {
	password, err := resolveVaultCLIPassword(false)
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(password)

	data, err := os.ReadFile(args[0]) //nolint:gosec // user-specified path
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	if !vault.IsAnsibleVault(data) {
		return fmt.Errorf("%s is not an Ansible Vault encrypted file", args[0])
	}

	plaintext, err := vault.Decrypt(data, password)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", args[0], err)
	}
	defer vault.ZeroBytes(plaintext)

	_, err = os.Stdout.Write(plaintext)
	return err
}

// resolveVaultCLIPassword resolves the vault password for CLI
// commands. Priority: --vault-password-file flag, config file,
// WTMCP_VAULT_PASSWORD env var, interactive prompt.
func resolveVaultCLIPassword(confirmOnPrompt bool) ([]byte, error) {
	if vaultPasswordFile != "" {
		return config.ReadPasswordFile(vaultPasswordFile)
	}

	if envPassword := os.Getenv("WTMCP_VAULT_PASSWORD"); envPassword != "" {
		os.Unsetenv("WTMCP_VAULT_PASSWORD") //nolint:errcheck // best-effort cleanup
		return []byte(envPassword), nil
	}

	workdir := config.WorkDir()
	if globalWorkdir != "" {
		workdir = globalWorkdir
	}
	cfg, err := config.Load("", workdir)
	if err == nil && cfg.Secrets.VaultPasswordFile != "" {
		resolve, vaultCloser := config.ResolveVaultPassword(cfg)
		defer func() { _ = vaultCloser.Close() }()
		password, err := resolve("")
		if err == nil {
			return password, nil
		}
	}

	if vaultAskPass || term.IsTerminal(syscall.Stdin) {
		return promptPassword(confirmOnPrompt)
	}

	return nil, fmt.Errorf("no vault password source — use --vault-password-file, " +
		"--ask-vault-pass, or set WTMCP_VAULT_PASSWORD")
}

func promptPassword(confirm bool) ([]byte, error) {
	fmt.Fprint(os.Stderr, "Vault password: ")
	password, err := term.ReadPassword(syscall.Stdin)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("read password: %w", err)
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("vault password must not be empty")
	}

	if confirm {
		fmt.Fprint(os.Stderr, "Confirm vault password: ")
		confirm, err := term.ReadPassword(syscall.Stdin)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			vault.ZeroBytes(password)
			return nil, fmt.Errorf("read confirmation: %w", err)
		}
		if !bytes.Equal(password, confirm) {
			vault.ZeroBytes(password)
			vault.ZeroBytes(confirm)
			return nil, fmt.Errorf("passwords do not match")
		}
		vault.ZeroBytes(confirm)
	}

	return password, nil
}
