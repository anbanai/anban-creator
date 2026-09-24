package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/repository"
)

const scheduleRecommendationGranularityMinutes = 15

type ScheduleRecommendation struct {
	Time               string `json:"time"`
	Timezone           string `json:"timezone"`
	GranularityMinutes int    `json:"granularity_minutes"`
	LoadBalanced       bool   `json:"load_balanced"`
}

type ScheduleRecommendationService struct {
	repo      repository.Repository
	catalogID string
	rule      billing.TaskTimePricing
	logger    *zerolog.Logger
}

func NewScheduleRecommendationService(repo repository.Repository, catalogID string, rule billing.TaskTimePricing, logger *zerolog.Logger) *ScheduleRecommendationService {
	return &ScheduleRecommendationService{repo: repo, catalogID: catalogID, rule: rule, logger: logger}
}

func (s *ScheduleRecommendationService) Recommend(ctx context.Context, userID string, now time.Time) ScheduleRecommendation {
	slots := preferredTaskOffPeakSlots(s.rule.OffPeakWindows)
	result := ScheduleRecommendation{Timezone: s.rule.Timezone, GranularityMinutes: scheduleRecommendationGranularityMinutes}
	if len(slots) == 0 {
		return result
	}
	location, err := time.LoadLocation(s.rule.Timezone)
	if err != nil {
		return result
	}
	localDate := now.In(location).Format("2006-01-02")
	nextRuns, err := s.repo.Plans().ListActiveNextRunAt(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("catalog_id", s.catalogID).Msg("schedule recommendation active plan aggregation failed")
		}
		result.Time = slots[stableScheduleIndex(s.catalogID, userID, localDate, len(slots))]
		return result
	}

	counts := make([]int, len(slots))
	for _, nextRun := range nextRuns {
		localNextRun := nextRun.In(location)
		minute := localNextRun.Hour()*60 + localNextRun.Minute()
		for index, slot := range slots {
			start, _ := serviceClockMinute(slot)
			if minuteInScheduleSlot(minute, start) {
				counts[index]++
				break
			}
		}
	}
	minimum := counts[0]
	for _, count := range counts[1:] {
		if count < minimum {
			minimum = count
		}
	}
	candidates := make([]string, 0, len(slots))
	for index, count := range counts {
		if count == minimum {
			candidates = append(candidates, slots[index])
		}
	}
	result.Time = candidates[stableScheduleIndex(s.catalogID, userID, localDate, len(candidates))]
	result.LoadBalanced = true
	return result
}

// Prefer evening through early morning, but only where the billing policy
// actually offers off-peak pricing. Balance within this pool before considering
// daytime: an empty lunch slot must not displace a valid night slot.
func preferredTaskOffPeakSlots(windows []billing.TimeWindow) []string {
	var nightWindows []billing.TimeWindow
	for _, window := range windows {
		start, startOK := serviceClockMinute(window.Start)
		end, endOK := serviceClockMinute(window.End)
		if !startOK || !endOK || start >= end {
			continue
		}
		for _, night := range [][2]int{{0, 9 * 60}, {18 * 60, 24 * 60}} {
			nightStart, nightEnd := max(start, night[0]), min(end, night[1])
			if nightStart < nightEnd {
				nightWindows = append(nightWindows, billing.TimeWindow{
					Start: fmt.Sprintf("%02d:%02d", nightStart/60, nightStart%60),
					End:   fmt.Sprintf("%02d:%02d", nightEnd/60, nightEnd%60),
				})
			}
		}
	}
	if slots := taskOffPeakSlots(nightWindows); len(slots) > 0 {
		return slots
	}
	return taskOffPeakSlots(windows)
}

func taskOffPeakSlots(windows []billing.TimeWindow) []string {
	const minutesPerDay = 24 * 60
	offPeak := make([]bool, minutesPerDay)
	for _, window := range windows {
		start, startOK := serviceClockMinute(window.Start)
		end, endOK := serviceClockMinute(window.End)
		if !startOK || !endOK || start >= end {
			continue
		}
		for minute := start; minute < end; minute++ {
			offPeak[minute] = true
		}
	}
	firstRunStart := -1
	for minute := 0; minute < minutesPerDay; minute++ {
		previous := (minute + minutesPerDay - 1) % minutesPerDay
		if offPeak[minute] && !offPeak[previous] {
			firstRunStart = minute
			break
		}
	}
	if firstRunStart < 0 {
		return nil
	}

	var slots []string
	visited := 0
	minute := firstRunStart
	for visited < minutesPerDay {
		if !offPeak[minute] {
			minute = (minute + 1) % minutesPerDay
			visited++
			continue
		}
		runStart := minute
		runLength := 0
		for visited+runLength < minutesPerDay && offPeak[(runStart+runLength)%minutesPerDay] {
			runLength++
		}
		for offset := 0; offset+scheduleRecommendationGranularityMinutes <= runLength; offset += scheduleRecommendationGranularityMinutes {
			slot := (runStart + offset) % minutesPerDay
			slots = append(slots, fmt.Sprintf("%02d:%02d", slot/60, slot%60))
		}
		minute = (runStart + runLength) % minutesPerDay
		visited += runLength
	}
	return slots
}

func minuteInScheduleSlot(minute, start int) bool {
	end := start + scheduleRecommendationGranularityMinutes
	if end <= 24*60 {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end-24*60
}

func stableScheduleIndex(catalogID, userID, localDate string, size int) int {
	if size <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(catalogID + "\x00" + userID + "\x00" + localDate))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(size))
}

func timeInWindows(value string, windows []billing.TimeWindow) bool {
	minute, ok := serviceClockMinute(value)
	if !ok {
		return false
	}
	for _, window := range windows {
		start, startOK := serviceClockMinute(window.Start)
		end, endOK := serviceClockMinute(window.End)
		if startOK && endOK && minute >= start && minute < end {
			return true
		}
	}
	return false
}
