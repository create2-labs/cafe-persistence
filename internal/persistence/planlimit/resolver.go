package planlimit

import (
	"fmt"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/repository"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
)

// Resolver reads plan scan limits for persistence completion quota (IMM-6b-4).
type Resolver struct {
	users repository.UserRepository
	plans repository.PlanRepository
}

// NewResolver creates a plan limit resolver backed by user and plan repositories.
func NewResolver(users repository.UserRepository, plans repository.PlanRepository) *Resolver {
	return &Resolver{users: users, plans: plans}
}

// ScanLimit returns the configured limit and whether the plan is unlimited for kind.
func (r *Resolver) ScanLimit(userID uuid.UUID, kind domain.ScanUsageKind) (limit int, unlimited bool, err error) {
	user, err := r.users.FindByID(userID.String())
	if err != nil {
		return 0, false, err
	}
	if user == nil {
		return 0, true, nil
	}
	plan, err := r.plans.FindByID(user.PlanID)
	if err != nil {
		return 0, false, err
	}
	if plan == nil {
		return 0, true, nil
	}
	switch kind {
	case domain.ScanUsageKindWallet:
		return plan.WalletScanLimit, plan.IsUnlimited(scan.PlanLimitKeyWallet), nil
	case domain.ScanUsageKindEndpoint:
		return plan.EndpointScanLimit, plan.IsUnlimited(scan.PlanLimitKeyEndpoint), nil
	default:
		return 0, false, fmt.Errorf("unsupported scan usage kind: %s", kind)
	}
}
