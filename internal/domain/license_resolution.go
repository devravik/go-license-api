package domain

import (
	"errors"
	"time"
)

func (l *License) ValidateHardRules() error {
	if l.Type != "plan" && l.Type != "product" {
		return errors.New("invalid_license_type")
	}
	if l.Type == "plan" {
		if l.PlanID == nil || *l.PlanID == "" {
			return errors.New("plan_id_required")
		}
		if l.ProductID != nil && *l.ProductID != "" {
			return errors.New("product_id_must_be_null_for_plan_type")
		}
		if len(l.Features) > 0 {
			return errors.New("features_must_be_empty_for_plan_type")
		}
	}
	if l.Type == "product" {
		if l.ProductID == nil || *l.ProductID == "" {
			return errors.New("product_id_required")
		}
		if l.PlanID != nil && *l.PlanID != "" {
			return errors.New("plan_id_must_be_null_for_product_type")
		}
		if len(l.Features) == 0 {
			return errors.New("features_required_for_product_type")
		}
		if l.Trial.Enabled {
			return errors.New("trial_must_be_disabled_for_product_type")
		}
	}
	if len(l.Features) > 0 && (len(l.Overrides.FeaturesAdd) > 0 || len(l.Overrides.FeaturesRemove) > 0) {
		return errors.New("full_override_cannot_be_combined_with_add_remove")
	}
	if l.Trial.Enabled && len(l.Trial.Features) == 0 {
		return errors.New("trial_features_required_when_trial_enabled")
	}
	if l.Trial.Enabled && l.Trial.EndsAt == nil {
		return errors.New("trial_ends_at_required_when_trial_enabled")
	}
	if l.SeatsTotal != -1 && l.SeatsUsed > l.SeatsTotal {
		return errors.New("seats_used_exceeds_seats_total")
	}
	return nil
}

func (l *License) ResolveFinalFeatures(plan *Plan, now time.Time) []string {
	base := make([]string, 0)
	if l.Type == "plan" && plan != nil {
		base = append(base, plan.Features...)
	}
	if l.Type == "product" {
		base = append(base, l.Features...)
	}
	if l.Trial.Enabled && l.Trial.EndsAt != nil && now.Before(*l.Trial.EndsAt) {
		if len(l.Trial.Features) > 0 {
			base = append([]string{}, l.Trial.Features...)
		}
	}
	if len(l.Features) > 0 {
		base = append([]string{}, l.Features...)
	} else {
		set := make(map[string]struct{}, len(base)+len(l.Overrides.FeaturesAdd))
		for _, f := range base {
			if f != "" {
				set[f] = struct{}{}
			}
		}
		for _, f := range l.Overrides.FeaturesAdd {
			if f != "" {
				set[f] = struct{}{}
			}
		}
		for _, f := range l.Overrides.FeaturesRemove {
			delete(set, f)
		}
		out := make([]string, 0, len(set))
		for f := range set {
			out = append(out, f)
		}
		base = out
	}
	l.FinalFeatures = base
	return base
}

