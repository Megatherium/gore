package sign

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Signer handles OpenPGP signing operations.
type Signer struct {
	entity            *openpgp.Entity
	signingKey        *packet.PrivateKey
	publicKeyOverride string
}

// NewSigner creates a new Signer from an armored private key and optional passphrase.
func NewSigner(armoredPrivateKey string, passphrase string) (*Signer, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	if len(keyring) == 0 {
		return nil, fmt.Errorf("no keys found in keyring")
	}

	entity := keyring[0]
	if entity.PrivateKey == nil {
		return nil, fmt.Errorf("key has no private key")
	}

	if entity.PrivateKey.Encrypted {
		if passphrase == "" {
			return nil, fmt.Errorf("private key is encrypted but no passphrase was provided")
		}
		if err := entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}
	}

	for _, sub := range entity.Subkeys {
		if sub.PrivateKey != nil && sub.PrivateKey.Encrypted {
			_ = sub.PrivateKey.Decrypt([]byte(passphrase))
		}
	}

	// Prefer a signing-capable key (primary or subkey) with a valid
	// self-signature; fall back to the primary key for exotic keyrings.
	signingKey := entity.PrivateKey
	if key, ok := entity.SigningKey(time.Now()); ok && key.PrivateKey != nil && !key.PrivateKey.Encrypted {
		signingKey = key.PrivateKey
	}

	return &Signer{entity: entity, signingKey: signingKey}, nil
}

// SetPublicKeyOverride configures an armored public key to serve instead of the
// one extracted from the private key. Returns an error if the key is invalid.
func (s *Signer) SetPublicKeyOverride(armoredPublicKey string) error {
	if _, err := ExtractPublicKey(armoredPublicKey); err != nil {
		return fmt.Errorf("invalid GPG_PUBLIC_KEY: %w", err)
	}
	s.publicKeyOverride = armoredPublicKey
	return nil
}

// SignCleartext creates an inline cleartext signed message (for InRelease).
func (s *Signer) SignCleartext(content string) (string, error) {
	var buf bytes.Buffer
	w, err := clearsign.Encode(&buf, s.signingKey, nil)
	if err != nil {
		return "", fmt.Errorf("clearsign encode error: %w", err)
	}

	if _, err := io.WriteString(w, content); err != nil {
		return "", fmt.Errorf("failed to write cleartext: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to close clearsign writer: %w", err)
	}

	return buf.String(), nil
}

// SignDetachedText creates an armored detached signature with sigclass 0x01 (text) for Release.gpg.
func (s *Signer) SignDetachedText(content string) (string, error) {
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSignText(&buf, s.entity, strings.NewReader(content), nil); err != nil {
		return "", fmt.Errorf("failed to sign detached text: %w", err)
	}
	return buf.String(), nil
}

// SignDetachedBinary creates an armored detached signature with sigclass 0x00 (binary) for repomd.xml.asc.
func (s *Signer) SignDetachedBinary(content []byte) (string, error) {
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, s.entity, bytes.NewReader(content), nil); err != nil {
		return "", fmt.Errorf("failed to sign detached binary: %w", err)
	}
	return buf.String(), nil
}

// PublicKey returns the ASCII-armored public key, honoring any configured override.
func (s *Signer) PublicKey() (string, error) {
	if s.publicKeyOverride != "" {
		return s.publicKeyOverride, nil
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create armor writer: %w", err)
	}

	if err := s.entity.Serialize(w); err != nil {
		return "", fmt.Errorf("failed to serialize public key: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to close armor writer: %w", err)
	}

	return buf.String(), nil
}

// Fingerprint returns the uppercase 40-hex-char fingerprint grouped in 4-character blocks.
func (s *Signer) Fingerprint() string {
	raw := fmt.Sprintf("%X", s.entity.PrimaryKey.Fingerprint)
	return FormatFingerprint(raw)
}

// FormatFingerprint formats a hex string into uppercase 4-character space-separated groups.
func FormatFingerprint(hexStr string) string {
	upper := strings.ToUpper(strings.ReplaceAll(hexStr, " ", ""))
	var groups []string
	for i := 0; i < len(upper); i += 4 {
		end := i + 4
		if end > len(upper) {
			end = len(upper)
		}
		groups = append(groups, upper[i:end])
	}
	return strings.Join(groups, " ")
}

// ExtractPublicKey extracts the armored public key from an armored private or public key string.
func ExtractPublicKey(armoredKey string) (string, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredKey))
	if err != nil {
		return "", fmt.Errorf("failed to read armored key: %w", err)
	}
	if len(keyring) == 0 {
		return "", fmt.Errorf("no keys found in armored key")
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := keyring[0].Serialize(w); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// GetKeyFingerprint extracts the formatted fingerprint from an armored private or public key.
func GetKeyFingerprint(armoredKey string) (string, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredKey))
	if err != nil {
		return "", fmt.Errorf("failed to read armored key: %w", err)
	}
	if len(keyring) == 0 {
		return "", fmt.Errorf("no keys found in armored key")
	}

	raw := fmt.Sprintf("%X", keyring[0].PrimaryKey.Fingerprint)
	return FormatFingerprint(raw), nil
}
