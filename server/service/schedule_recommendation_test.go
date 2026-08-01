package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type recommendationPlanRepository struct {
	repository.PlanRepository
	plans []*model.Plan
	err   error
}

func (r recommendationPlanRepository) ListActive(context.Context) ([]*model.Plan, error) {
	return r.plans, r.err
}

func (r recommendationPlanRepository) ListActiveNextRunAt(context.Context) ([]time.Time, error) {
	if r.err != nil {
		return nil, r.err
	}
	values := make([]time.Time, 0, len(r.plans))
	for _, plan := range r.plans {
		if plan != nil && plan.NextRunAt != nil {
			values = append(values, *plan.NextRunAt)
		}
	}
	return values, nil
}

type recommendationRepository struct {
	repository.Repository
	plans repository.PlanRepository
}

func (r recommendationRepository) Plans() repository.PlanRepository { return r.plans }

func TestScheduleRecommendationBalancesOnlyOffPeakActiveRuns(t *testing.T) {
	rule := billing.TaskTimePricing{
		Timezone: "Asia/Shanghai", OffPeakRatePercent: 80,
		PeakWindows:    []billing.TimeWindow{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}},
		OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "14:00"}, {Start: "18:00", End: "24:00"}},
	}
	local := time.FixedZone("CST", 8*60*60)
	busy := time.Date(2026, 7, 31, 0, 0, 0, 0, local)
	repo := recommendationRepository{plans: recommendationPlanRepository{plans: []*model.Plan{
		{Status: model.PlanStatusActive, NextRunAt: &busy},
	}}}
	svc := NewScheduleRecommendationService(repo, "retail-test", rule, nil)
	got := svc.Recommend(context.Background(), "user-a", time.Date(2026, 7, 31, 3, 0, 0, 0, time.UTC))
	if got.Time == "" || got.Time == "00:00" || got.Timezone != rule.Timezone || got.GranularityMinutes != 15 || !got.LoadBalanced {
		t.Fatalf("recommendation = %#v", got)
	}
	if !timeInWindows(got.Time, rule.OffPeakWindows) {
		t.Fatalf("recommended peak time %q", got.Time)
	}
}

func TestScheduleRecommendationIsStableAndFallsBackOnReadFailure(t *testing.T) {
	rule := billing.TaskTimePricing{Timezone: "Asia/Shanghai", OffPeakRatePercent: 80, OffPeakWindows: []billing.TimeWindow{{Start: "12:00", End: "13:00"}}}
	repo := recommendationRepository{plans: recommendationPlanRepository{err: errors.New("database unavailable")}}
	svc := NewScheduleRecommendationService(repo, "retail-test", rule, nil)
	now := time.Date(2026, 7, 31, 3, 0, 0, 0, time.UTC)
	first := svc.Recommend(context.Background(), "user-a", now)
	second := svc.Recommend(context.Background(), "user-a", now)
	if first != second || first.LoadBalanced || first.Time == "" || !timeInWindows(first.Time, rule.OffPeakWindows) {
		t.Fatalf("fallback first=%#v second=%#v", first, second)
	}
}

func TestTaskOffPeakSlotsIncludesSlotSpanningMidnight(t *testing.T) {
	windows := []billing.TimeWindow{{Start: "00:00", End: "00:10"}, {Start: "23:55", End: "24:00"}}
	got := taskOffPeakSlots(windows)
	if len(got) != 1 || got[0] != "23:55" {
		t.Fatalf("slots = %#v", got)
	}
}

func TestTaskOffPeakSlotsCutsContinuousMidnightWindowWithoutOverlap(t *testing.T) {
	windows := []billing.TimeWindow{{Start: "00:00", End: "00:10"}, {Start: "23:40", End: "24:00"}}
	got := taskOffPeakSlots(windows)
	want := []string{"23:40", "23:55"}
	if len(got) != len(want) {
		t.Fatalf("slots = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("slots = %#v, want %#v", got, want)
		}
	}
}
