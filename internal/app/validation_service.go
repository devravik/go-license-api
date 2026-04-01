package app

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

type ValidationService interface {
	Validate(ctx context.Context, key, product string) (*domain.ValidationResult, error)
}

type validationService struct{}

func NewValidationService() ValidationService {
	return &validationService{}
}

func (s *validationService) Validate(ctx context.Context, key, product string) (*domain.ValidationResult, error) {
	// Mock implementation for now as per Task 6 focus on handler
	if key == "" {
		return &domain.ValidationResult{
			Valid: false,
			Error: "invalid_key",
		}, nil
	}

	return &domain.ValidationResult{
		Valid: true,
		Meta: map[string]interface{}{
			"product": product,
			"plan":    "professional",
		},
	}, nil
}
