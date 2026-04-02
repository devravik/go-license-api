package domain

import "errors"

var (
	ErrLicenseNotFound    = errors.New("license not found")
	ErrLicenseExpired     = errors.New("license expired")
	ErrLicenseRevoked     = errors.New("license revoked")
	ErrLicenseGracePeriod = errors.New("license in grace period")
	ErrSeatLimitReached   = errors.New("seat limit reached")
	ErrAlreadyActivated   = errors.New("machine already has active activation")
	ErrProductNotFound    = errors.New("product not found")
	ErrInvalidTenant      = errors.New("invalid tenant")
	ErrTenantSuspended    = errors.New("tenant suspended")
	ErrIPNotAllowed       = errors.New("ip not in allowlist")
	ErrKeyExpired         = errors.New("api key expired")
)
