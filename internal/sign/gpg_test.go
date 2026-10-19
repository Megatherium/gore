package sign_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/leoherzog/reprox/internal/sign"
)

func generateTestKey(t *testing.T, passphrase string) (string, string) {
	t.Helper()

	entity, err := openpgp.NewEntity("Test User", "Reprox Test", "test@example.com", nil)
	if err != nil {
		t.Fatalf("openpgp.NewEntity error: %v", err)
	}

	if passphrase != "" {
		if err := entity.PrivateKey.Encrypt([]byte(passphrase)); err != nil {
			t.Fatalf("entity.PrivateKey.Encrypt error: %v", err)
		}
		for _, sub := range entity.Subkeys {
			if sub.PrivateKey != nil {
				_ = sub.PrivateKey.Encrypt([]byte(passphrase))
			}
		}
	}

	var privBuf bytes.Buffer
	pw, err := armor.Encode(&privBuf, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode private error: %v", err)
	}

	if passphrase != "" {
		if err := entity.SerializePrivateWithoutSigning(pw, nil); err != nil {
			t.Fatalf("SerializePrivateWithoutSigning error: %v", err)
		}
	} else {
		if err := entity.SerializePrivate(pw, nil); err != nil {
			t.Fatalf("SerializePrivate error: %v", err)
		}
	}
	pw.Close()

	var pubBuf bytes.Buffer
	uw, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode public error: %v", err)
	}
	if err := entity.Serialize(uw); err != nil {
		t.Fatalf("Serialize public error: %v", err)
	}
	uw.Close()

	return privBuf.String(), pubBuf.String()
}

func TestGpgSigning(t *testing.T) {
	privKey, pubKey := generateTestKey(t, "")

	signer, err := sign.NewSigner(privKey, "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}

	t.Run("SignCleartext", func(t *testing.T) {
		content := "Origin: reprox\nSuite: stable\n"
		signed, err := signer.SignCleartext(content)
		if err != nil {
			t.Fatalf("SignCleartext error: %v", err)
		}

		if !strings.Contains(signed, "-----BEGIN PGP SIGNED MESSAGE-----") {
			t.Errorf("missing cleartext header")
		}
		if !strings.Contains(signed, content) {
			t.Errorf("missing original content")
		}

		block, _ := clearsign.Decode([]byte(signed))
		if block == nil {
			t.Fatalf("failed to decode cleartext block")
		}

		keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(pubKey))
		if err != nil {
			t.Fatalf("ReadArmoredKeyRing error: %v", err)
		}

		_, err = openpgp.CheckDetachedSignature(keyring, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
		if err != nil {
			t.Fatalf("signature verification failed: %v", err)
		}
	})

	t.Run("SignDetachedText", func(t *testing.T) {
		content := "Release file text content"
		sig, err := signer.SignDetachedText(content)
		if err != nil {
			t.Fatalf("SignDetachedText error: %v", err)
		}

		if !strings.Contains(sig, "-----BEGIN PGP SIGNATURE-----") {
			t.Errorf("missing detached signature header")
		}

		keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(pubKey))
		if err != nil {
			t.Fatalf("ReadArmoredKeyRing error: %v", err)
		}

		_, err = openpgp.CheckArmoredDetachedSignature(keyring, strings.NewReader(content), strings.NewReader(sig), nil)
		if err != nil {
			t.Fatalf("detached text signature verification failed: %v", err)
		}
	})

	t.Run("SignDetachedBinary", func(t *testing.T) {
		content := []byte("<xml>repomd</xml>")
		sig, err := signer.SignDetachedBinary(content)
		if err != nil {
			t.Fatalf("SignDetachedBinary error: %v", err)
		}

		keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(pubKey))
		if err != nil {
			t.Fatalf("ReadArmoredKeyRing error: %v", err)
		}

		_, err = openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(content), strings.NewReader(sig), nil)
		if err != nil {
			t.Fatalf("detached binary signature verification failed: %v", err)
		}
	})

	t.Run("Fingerprint", func(t *testing.T) {
		fp := signer.Fingerprint()
		parts := strings.Split(fp, " ")
		if len(parts) != 10 {
			t.Errorf("expected 10 groups of 4 hex chars, got %d in %q", len(parts), fp)
		}
		for _, part := range parts {
			if len(part) != 4 {
				t.Errorf("expected group length 4, got %q", part)
			}
		}

		fpFromPub, err := sign.GetKeyFingerprint(pubKey)
		if err != nil {
			t.Fatalf("GetKeyFingerprint error: %v", err)
		}
		if fpFromPub != fp {
			t.Errorf("fingerprint mismatch: got %s, want %s", fpFromPub, fp)
		}
	})
}

func TestEncryptedKey(t *testing.T) {
	pass := "secret-passphrase"
	privKey, _ := generateTestKey(t, pass)

	// Fails with no passphrase
	_, err := sign.NewSigner(privKey, "")
	if err == nil {
		t.Fatalf("expected error without passphrase")
	}

	// Succeeds with passphrase
	signer, err := sign.NewSigner(privKey, pass)
	if err != nil {
		t.Fatalf("NewSigner with passphrase error: %v", err)
	}

	signed, err := signer.SignCleartext("secure content")
	if err != nil {
		t.Fatalf("SignCleartext error: %v", err)
	}
	if !strings.Contains(signed, "-----BEGIN PGP SIGNED MESSAGE-----") {
		t.Errorf("missing signature")
	}
}
