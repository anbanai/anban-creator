package service

import (
	"context"
	"errors"
	"fmt"
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

func TestScheduleRecommendationPrefersNightOffPeak(t *testing.T) {
	for _, tt := range []struct {
		name    string
		windows []billing.TimeWindow
		busy    []int
		readErr error
		want    string
	}{
		{
			name:    "night before empty noon",
			windows: []billing.TimeWindow{{Start: "12:00", End: "14:00"}, {Start: "22:00", End: "22:15"}},
			busy:    []int{22 * 60}, want: "22:00",
		},
		{
			name:    "balance UTC runs in the pricing timezone",
			windows: []billing.TimeWindow{{Start: "12:00", End: "14:00"}, {Start: "22:00", End: "22:30"}},
			busy:    []int{22 * 60}, want: "22:15",
		},
		{
			name:    "early morning before empty noon",
			windows: []billing.TimeWindow{{Start: "02:00", End: "02:15"}, {Start: "12:00", End: "14:00"}},
			busy:    []int{2 * 60}, want: "02:00",
		},
		{
			name:    "read failure still prefers night",
			windows: []billing.TimeWindow{{Start: "12:00", End: "14:00"}, {Start: "23:00", End: "23:15"}},
			readErr: errors.New("database unavailable"), want: "23:00",
		},
		{
			name:    "daytime fallback when no night low peak exists",
			windows: []billing.TimeWindow{{Start: "12:00", End: "12:15"}}, want: "12:00",
		},
		{
			name:    "night fragment cannot fit a full slot",
			windows: []billing.TimeWindow{{Start: "12:00", End: "12:15"}, {Start: "23:00", End: "23:10"}}, want: "12:00",
		},
		{
			name:    "clip low peak window to evening boundary",
			windows: []billing.TimeWindow{{Start: "17:50", End: "18:15"}}, want: "18:00",
		},
		{
			name:    "clip low peak window to morning boundary",
			windows: []billing.TimeWindow{{Start: "08:45", End: "10:00"}}, want: "08:45",
		},
		{
			name:    "night slot across midnight",
			windows: []billing.TimeWindow{{Start: "00:00", End: "00:10"}, {Start: "12:00", End: "14:00"}, {Start: "23:55", End: "24:00"}},
			want:    "23:55",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			location, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				t.Fatal(err)
			}
			plans := &recommendationPlanRepository{err: tt.readErr}
			for _, minute := range tt.busy {
				// Persist UTC to verify that balancing uses the pricing timezone.
				run := time.Date(2026, 9, 24, minute/60, minute%60, 0, 0, location).UTC()
				plans.plans = append(plans.plans, &model.Plan{NextRunAt: &run})
			}
			rule := billing.TaskTimePricing{Timezone: "Asia/Shanghai", OffPeakWindows: tt.windows}
			svc := NewScheduleRecommendationService(recommendationRepository{plans: plans}, "retail-test", rule, nil)
			for i := 0; i < 100; i++ {
				got := svc.Recommend(t.Context(), fmt.Sprintf("user-%d", i), time.Date(2026, 9, 24, 4, 0, 0, 0, time.UTC))
				if got.Time != tt.want || got.LoadBalanced != (tt.readErr == nil) {
					t.Fatalf("recommendation = %#v, want time %s, balanced %v", got, tt.want, tt.readErr == nil)
				}
			}
		})
	}
}

func TestScheduleRecommendationSpreadsNewPlansAcrossNight(t *testing.T) {
	rule := billing.TaskTimePricing{
		Timezone:       "Asia/Shanghai",
		OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "14:00"}, {Start: "18:00", End: "24:00"}},
	}
	plans := &recommendationPlanRepository{}
	svc := NewScheduleRecommendationService(recommendationRepository{plans: plans}, "retail-test", rule, nil)
	counts := map[string]int{}
	// 15 night hours have 60 quarter-hour slots. Two rounds should put
	// exactly two plans in every slot, even for the same user on the same day.
	for i := 0; i < 120; i++ {
		got := svc.Recommend(t.Context(), "user-a", time.Date(2026, 9, 24, 4, 0, 0, 0, time.UTC))
		run, err := time.Parse(time.RFC3339, "2026-09-24T"+got.Time+":00+08:00")
		if err != nil {
			t.Fatal(err)
		}
		if run.Hour() >= 9 && run.Hour() < 18 || run.Minute()%15 != 0 || !got.LoadBalanced {
			t.Fatalf("expected balanced night recommendation, got %#v", got)
		}
		counts[got.Time]++
		plans.plans = append(plans.plans, &model.Plan{NextRunAt: &run})
	}
	if len(counts) != 60 {
		t.Fatalf("used %d night slots, want 60", len(counts))
	}
	for slot, count := range counts {
		if count != 2 {
			t.Fatalf("slot %s has %d plans, want 2", slot, count)
		}
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
