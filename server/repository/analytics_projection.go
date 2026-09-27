package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sort"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm/clause"
)

// InsertObservations amortizes identity checks, typed facts and raw writes over
// bounded chunks. The project row must already be locked by the transaction.
func (r *AnalyticsRepository) InsertObservations(ctx context.Context, contents []model.AnalyticsContent, observations []model.AnalyticsObservation, raw map[string]string) ([]AnalyticsAffectedDay, error) {
	unique := map[string]model.AnalyticsContent{}
	for _, c := range contents {
		unique[c.ID] = c
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		var existing []model.AnalyticsContent
		if e := r.db.WithContext(ctx).Select("id,project_id").Where("id IN ?", part).Find(&existing).Error; e != nil {
			return nil, e
		}
		for _, c := range existing {
			if unique[c.ID].ProjectID != c.ProjectID {
				return nil, errors.New("analytics content belongs to another project")
			}
		}
		rows := make([]model.AnalyticsContent, 0, len(part))
		for _, id := range part {
			rows = append(rows, unique[id])
		}
		if e := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"title", "content_type", "status", "url", "date", "task_id", "publication_id", "post_id", "updated_at"})}).CreateInBatches(rows, 500).Error; e != nil {
			return nil, e
		}
	}
	affected := map[AnalyticsAffectedDay]bool{}
	for start := 0; start < len(observations); start += 500 {
		end := start + 500
		if end > len(observations) {
			end = len(observations)
		}
		part := observations[start:end]
		ids := make([]string, 0, len(part))
		for _, o := range part {
			ids = append(ids, o.ID)
		}
		var old []model.AnalyticsObservation
		if e := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&old).Error; e != nil {
			return nil, e
		}
		seen := map[string]model.AnalyticsObservation{}
		storedRaw := map[string]string{}
		if len(old) > 0 {
			var payloads []model.AnalyticsRawPayload
			if e := r.db.WithContext(ctx).Where("observation_id IN ?", ids).Find(&payloads).Error; e != nil {
				return nil, e
			}
			for _, p := range payloads {
				storedRaw[p.ObservationID] = p.Payload
			}
		}
		for _, o := range old {
			seen[o.ID] = o
		}
		fresh := []model.AnalyticsObservation{}
		payloads := []model.AnalyticsRawPayload{}
		for _, o := range part {
			if prior, ok := seen[o.ID]; ok {
				prior.Sequence = 0
				prior.RevokedAt = nil
				o.Sequence = 0
				o.RevokedAt = nil
				if !analyticsObservationEqual(prior, o) || sha256.Sum256([]byte(storedRaw[o.ID])) != sha256.Sum256([]byte(raw[o.ID])) {
					return nil, ErrAnalyticsIdempotencyConflict
				}
				continue
			}
			o.Sequence = 0
			seen[o.ID] = o
			fresh = append(fresh, o)
			payloads = append(payloads, model.AnalyticsRawPayload{ObservationID: o.ID, Payload: raw[o.ID]})
			affected[AnalyticsAffectedDay{ContentID: o.ContentID, MetricBasis: o.MetricBasis, StatDate: o.StatDate}] = true
		}
		if len(fresh) > 0 {
			if e := r.db.WithContext(ctx).CreateInBatches(fresh, 500).Error; e != nil {
				return nil, e
			}
			if e := r.db.WithContext(ctx).CreateInBatches(payloads, 500).Error; e != nil {
				return nil, e
			}
		}
	}
	result := make([]AnalyticsAffectedDay, 0, len(affected))
	for key := range affected {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		return a.ContentID+a.MetricBasis+a.StatDate < b.ContentID+b.MetricBasis+b.StatDate
	})
	return result, nil
}
func analyticsObservationEqual(a, b model.AnalyticsObservation) bool {
	return a.ID == b.ID && a.ProjectID == b.ProjectID && a.ContentID == b.ContentID && a.BatchID == b.BatchID && a.TrackingID == b.TrackingID && a.MetricBasis == b.MetricBasis && a.StatDate == b.StatDate && a.Source == b.Source && a.SourcePriority == b.SourcePriority && a.EffectiveAt.Equal(b.EffectiveAt) && a.ReceivedAt.Equal(b.ReceivedAt) && reflect.DeepEqual(a.AnalyticsMetrics, b.AnalyticsMetrics)
}

type analyticsProjectionGroup struct{ Basis, Granularity, Start, End string }

// RecomputeDays updates each affected projection once. Winner selection and
// rollups run in SQL over at most 500 content identities per chunk. Account
// buckets are updated once after all content rows, not once per import row.
func (r *AnalyticsRepository) RecomputeDays(ctx context.Context, project string, generation int64, days []AnalyticsAffectedDay) error {
	groups := map[analyticsProjectionGroup]map[string]bool{}
	for _, day := range days {
		for _, gran := range []string{"day", "week", "month"} {
			start, end := AnalyticsBucketBounds(day.StatDate, gran)
			g := analyticsProjectionGroup{day.MetricBasis, gran, start, end}
			if groups[g] == nil {
				groups[g] = map[string]bool{}
			}
			groups[g][day.ContentID] = true
		}
	}
	keys := make([]analyticsProjectionGroup, 0, len(groups))
	for g := range groups {
		keys = append(keys, g)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		order := map[string]int{"day": 0, "week": 1, "month": 2}
		if a.Granularity != b.Granularity {
			return order[a.Granularity] < order[b.Granularity]
		}
		return a.Basis+a.Start < b.Basis+b.Start
	})
	for _, g := range keys {
		ids := make([]string, 0, len(groups[g]))
		for id := range groups[g] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			part := ids[start:end]
			rows := []model.AnalyticsBucket{}
			if g.Granularity == "day" {
				// Backfills receive new storage sequences. Use their original receipt
				// time before sequence so an old import cannot replace a later correction.
				ranked := r.db.WithContext(ctx).Model(&model.AnalyticsObservation{}).Select("content_id, id AS observation_id, stat_date AS last_stat_date,"+analyticsMetricColumns("")+", ROW_NUMBER() OVER (PARTITION BY content_id ORDER BY source_priority DESC, effective_at DESC, received_at DESC, sequence DESC) AS rn").Where("project_id = ? AND metric_basis = ? AND stat_date = ? AND revoked_at IS NULL AND content_id IN ?", project, g.Basis, g.Start, part)
				if e := r.db.WithContext(ctx).Table("(?) AS winners", ranked).Where("rn = 1").Scan(&rows).Error; e != nil {
					return e
				}
			} else {
				q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND metric_basis = ? AND granularity = 'day' AND bucket_start BETWEEN ? AND ? AND content_id IN ?", project, generation, g.Basis, g.Start, g.End, part)
				if g.Basis == "cumulative" {
					ranked := q.Select("content_id,observation_id,last_stat_date," + analyticsMetricColumns("") + ", ROW_NUMBER() OVER (PARTITION BY content_id ORDER BY bucket_start DESC) AS rn")
					if e := r.db.WithContext(ctx).Table("(?) AS winners", ranked).Where("rn = 1").Scan(&rows).Error; e != nil {
						return e
					}
				} else {
					if e := q.Select("content_id,MAX(last_stat_date) AS last_stat_date," + analyticsSumColumns("")).Group("content_id").Scan(&rows).Error; e != nil {
						return e
					}
				}
			}
			if e := r.db.WithContext(ctx).Where("project_id = ? AND generation = ? AND metric_basis = ? AND granularity = ? AND bucket_start = ? AND content_id IN ?", project, generation, g.Basis, g.Granularity, g.Start, part).Delete(&model.AnalyticsBucket{}).Error; e != nil {
				return e
			}
			for i := range rows {
				b := &rows[i]
				b.ProjectID = project
				b.Generation = generation
				b.MetricBasis = g.Basis
				b.Granularity = g.Granularity
				b.BucketStart = g.Start
				b.Coverage = 1
			}
			if len(rows) > 0 {
				if e := r.db.WithContext(ctx).CreateInBatches(rows, 500).Error; e != nil {
					return e
				}
			}
		}
	}
	for _, g := range keys {
		account := model.AnalyticsBucket{ProjectID: project, Generation: generation, MetricBasis: g.Basis, Granularity: g.Granularity, BucketStart: g.Start}
		var totals model.AnalyticsBucket
		if e := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND metric_basis = ? AND granularity = ? AND bucket_start = ? AND content_id <> ''", project, generation, g.Basis, g.Granularity, g.Start).Select(analyticsSumColumns("") + ", COUNT(*) AS coverage, MAX(last_stat_date) AS last_stat_date").Scan(&totals).Error; e != nil {
			return e
		}
		account.AnalyticsMetrics = totals.AnalyticsMetrics
		account.Coverage = totals.Coverage
		account.LastStatDate = totals.LastStatDate
		if e := r.replaceBucket(ctx, &account, account.Coverage > 0); e != nil {
			return e
		}
	}
	return nil
}
