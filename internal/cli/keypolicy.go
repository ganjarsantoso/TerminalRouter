package cli

import (
	"fmt"
	"strings"

	"github.com/termrouter/termrouter/internal/storage"
)

// KeyPolicyValidationContext controls portable-key validation behaviour.
type KeyPolicyValidationContext struct {
	PublicHosting          bool
	UnsafePortableOverride bool
}

// ValidateEffectiveClientKeyPolicy enforces mandatory controls on portable keys.
// In public-hosting mode a portable key MUST have at least one allowed alias,
// a positive MaxRequestBody, and a positive DailyEstimatedCostUSD.
// The UnsafePortableOverride flag is the only escape hatch and must be passed
// deliberately. Non-portable keys always pass.
func ValidateEffectiveClientKeyPolicy(key storage.ClientKey, ctx KeyPolicyValidationContext) error {
	if !key.Portable {
		return nil
	}
	if ctx.PublicHosting && !ctx.UnsafePortableOverride {
		missing := portableMissingControls(key)
		if len(missing) > 0 {
			return &PortableKeyPolicyError{Missing: missing}
		}
	}
	return nil
}

// PortableKeyPolicyError is returned when a portable key violates mandatory
// controls in public-hosting mode. Its stable error code is
// "portable_key_policy_unsafe".
type PortableKeyPolicyError struct {
	Missing []string
}

func (e *PortableKeyPolicyError) Error() string {
	return fmt.Sprintf("portable_key_policy_unsafe: refusing to write unrestricted portable key in public-hosting mode; missing %s (or pass --unsafe-unrestricted-portable to override)",
		strings.Join(e.Missing, ", "))
}
