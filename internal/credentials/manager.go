package credentials

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Manager resolves and stores provider credentials.
type Manager struct {
	backend   string
	vaultPath string
	vaultKey  []byte // derived; nil if not vault
	mu        sync.RWMutex
	// in-memory vault when file vault is used (encrypted at rest on disk)
	mem map[string]string
}

// NewManager creates a credential manager for the given backend.
// For vault backend, vaultPath is the vault.db path and passphrase unlocks it.
// Empty passphrase for vault uses a machine-local key file when available.
func NewManager(backend, vaultPath, passphrase string) (*Manager, error) {
	m := &Manager{
		backend:   backend,
		vaultPath: vaultPath,
		mem:       map[string]string{},
	}
	switch backend {
	case "vault":
		if err := m.initVault(passphrase); err != nil {
			return nil, err
		}
	case "keyring", "env":
		// no init
	default:
		return nil, fmt.Errorf("unknown credential backend %q", backend)
	}
	return m, nil
}

func (m *Manager) initVault(passphrase string) error {
	if passphrase == "" {
		// Use or create a local key file next to the vault (0o600).
		keyPath := m.vaultPath + ".key"
		if b, err := os.ReadFile(keyPath); err == nil {
			m.vaultKey = b
		} else {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			if err := os.WriteFile(keyPath, key, 0o600); err != nil {
				return err
			}
			m.vaultKey = key
		}
	} else {
		salt := sha256.Sum256([]byte("termrouter-vault-v1"))
		m.vaultKey = argon2.IDKey([]byte(passphrase), salt[:], 3, 64*1024, 4, 32)
	}
	return m.loadVault()
}

// Resolve returns the secret for a credential reference.
// Supported: env://VAR, vault://name, keyring://service/user, none://
func (m *Manager) Resolve(ref string) (string, error) {
	if ref == "" || ref == "none://" {
		return "", nil
	}
	scheme, rest, ok := strings.Cut(ref, "://")
	if !ok {
		return "", fmt.Errorf("invalid credential_ref %q", ref)
	}
	switch scheme {
	case "env":
		v := os.Getenv(rest)
		if v == "" {
			return "", fmt.Errorf("environment variable %q is not set", rest)
		}
		return v, nil
	case "vault":
		m.mu.RLock()
		defer m.mu.RUnlock()
		v, ok := m.mem[rest]
		if !ok || v == "" {
			return "", fmt.Errorf("vault secret %q not found", rest)
		}
		return v, nil
	case "keyring":
		return resolveKeyring(rest)
	case "none":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported credential scheme %q", scheme)
	}
}

// Store saves a secret under the active backend and returns the credential_ref.
func (m *Manager) Store(name, secret string) (string, error) {
	switch m.backend {
	case "env":
		// Store as env reference; caller must set the env var.
		envName := "TERMROUTER_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		_ = os.Setenv(envName, secret)
		return "env://" + envName, nil
	case "vault":
		m.mu.Lock()
		m.mem[name] = secret
		m.mu.Unlock()
		if err := m.saveVault(); err != nil {
			return "", err
		}
		return "vault://" + name, nil
	case "keyring":
		if err := storeKeyring(name, secret); err != nil {
			// Fall back to vault-style file if keyring unavailable.
			return m.storeVaultFallback(name, secret)
		}
		return "keyring://termrouter/" + name, nil
	default:
		return "", fmt.Errorf("unknown backend %q", m.backend)
	}
}

func (m *Manager) storeVaultFallback(name, secret string) (string, error) {
	if m.vaultKey == nil {
		if err := m.initVault(""); err != nil {
			return "", fmt.Errorf("keyring unavailable and vault init failed: %w", err)
		}
	}
	m.mu.Lock()
	m.mem[name] = secret
	m.mu.Unlock()
	if err := m.saveVault(); err != nil {
		return "", err
	}
	return "vault://" + name, nil
}

// Remove deletes a stored secret if backend supports it.
func (m *Manager) Remove(ref string) error {
	scheme, rest, ok := strings.Cut(ref, "://")
	if !ok {
		return nil
	}
	switch scheme {
	case "vault":
		m.mu.Lock()
		delete(m.mem, rest)
		m.mu.Unlock()
		return m.saveVault()
	case "keyring":
		return removeKeyring(rest)
	default:
		return nil
	}
}

// ListVaultNames returns names stored in the encrypted vault.
func (m *Manager) ListVaultNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.mem))
	for k := range m.mem {
		out = append(out, k)
	}
	return out
}

// --- vault file format: base64(nonce||ciphertext) JSON map encrypted ---

func (m *Manager) saveVault() error {
	if m.vaultKey == nil {
		return fmt.Errorf("vault not initialized")
	}
	m.mu.RLock()
	// serialize as name=value lines using raw base64 (no padding) so '=' is a safe delimiter
	var b strings.Builder
	for k, v := range m.mem {
		b.WriteString(base64.RawStdEncoding.EncodeToString([]byte(k)))
		b.WriteByte('=')
		b.WriteString(base64.RawStdEncoding.EncodeToString([]byte(v)))
		b.WriteByte('\n')
	}
	plaintext := []byte(b.String())
	m.mu.RUnlock()

	aead, err := chacha20poly1305.NewX(m.vaultKey)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := aead.Seal(nonce, nonce, plaintext, nil)
	return os.WriteFile(m.vaultPath, ct, 0o600)
}

func (m *Manager) loadVault() error {
	data, err := os.ReadFile(m.vaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	aead, err := chacha20poly1305.NewX(m.vaultKey)
	if err != nil {
		return err
	}
	if len(data) < aead.NonceSize() {
		return fmt.Errorf("vault file corrupted")
	}
	nonce, ct := data[:aead.NonceSize()], data[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("vault decrypt failed (wrong passphrase?): %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem = map[string]string{}
	for _, line := range strings.Split(string(pt), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kb, err1 := base64.RawStdEncoding.DecodeString(k)
		vb, err2 := base64.RawStdEncoding.DecodeString(v)
		if err1 != nil || err2 != nil {
			// tolerate padded legacy entries
			kb, err1 = base64.StdEncoding.DecodeString(k)
			vb, err2 = base64.StdEncoding.DecodeString(v)
			if err1 != nil || err2 != nil {
				continue
			}
		}
		m.mem[string(kb)] = string(vb)
	}
	return nil
}

// --- Client API key hashing ---

// GenerateClientKey creates a new tr_live_ key and its storage fields.
func GenerateClientKey() (plaintext string, prefix string, hash string, salt string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	secret := hex.EncodeToString(raw)
	plaintext = "tr_live_" + secret
	prefix = plaintext[:16] // tr_live_ + 8 hex
	saltBytes := make([]byte, 16)
	if _, err = rand.Read(saltBytes); err != nil {
		return
	}
	salt = base64.RawStdEncoding.EncodeToString(saltBytes)
	hash = HashClientKey(plaintext, salt)
	return
}

// ClientKeyLookupPrefix returns the non-secret lookup prefix for a presented
// token, matching the stored key_prefix (tr_live_ + 8 hex = 16 chars). Returns
// "" when the token is not a well-formed tr_live_ key.
func ClientKeyLookupPrefix(token string) string {
	const p = "tr_live_"
	if !strings.HasPrefix(token, p) || len(token) < 16 {
		return ""
	}
	return token[:16]
}

// HashClientKey derives a hash for a client key using Argon2id.
func HashClientKey(plaintext, saltB64 string) string {
	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		salt = []byte(saltB64)
	}
	key := argon2.IDKey([]byte(plaintext), salt, 2, 32*1024, 2, 32)
	return base64.RawStdEncoding.EncodeToString(key)
}

// VerifyClientKey checks a presented key against stored salt+hash.
func VerifyClientKey(plaintext, salt, hash string) bool {
	computed := HashClientKey(plaintext, salt)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

// RedactSecret masks a secret for display.
func RedactSecret(s string) string {
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:4] + strings.Repeat("•", 12) + s[len(s)-4:]
}
