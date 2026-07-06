package identity

import (
	"fmt"
	"strings"

	bip39 "github.com/tyler-smith/go-bip39"
)

// Recovery phrase: the 32-byte identity seed is the whole account, so a BIP39
// mnemonic of it is a complete, human-writable backup. Enter it on a new
// device (or after a forgotten passphrase) to reproduce the exact same
// identity, guilds, and message keys — everything derives from the seed.

// Mnemonic returns this identity's 24-word BIP39 recovery phrase.
func (id *Identity) Mnemonic() (string, error) {
	m, err := bip39.NewMnemonic(id.Seed())
	if err != nil {
		return "", fmt.Errorf("identity: encode mnemonic: %w", err)
	}
	return m, nil
}

// SeedFromMnemonic decodes a BIP39 recovery phrase back to the 32-byte seed,
// validating the checksum. Whitespace and case are normalized.
func SeedFromMnemonic(phrase string) ([]byte, error) {
	norm := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(phrase))), " ")
	if !bip39.IsMnemonicValid(norm) {
		return nil, fmt.Errorf("identity: that recovery phrase isn't valid — check the words and order")
	}
	seed, err := bip39.EntropyFromMnemonic(norm)
	if err != nil {
		return nil, fmt.Errorf("identity: decode mnemonic: %w", err)
	}
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("identity: recovery phrase must encode a %d-byte seed", SeedSize)
	}
	return seed, nil
}

// RestoreKeystore reconstructs an identity from a recovery phrase and writes it
// to a new keystore sealed under passphrase. The rest of the account (guilds,
// history, mailbox) is then recovered by logging in and syncing from peers.
func RestoreKeystore(path, phrase, passphrase string) error {
	seed, err := SeedFromMnemonic(phrase)
	if err != nil {
		return err
	}
	id, err := FromSeed(seed)
	if err != nil {
		return err
	}
	return SaveKeystore(path, passphrase, id)
}
