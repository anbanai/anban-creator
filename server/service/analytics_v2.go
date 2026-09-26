package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

var ErrAnalyticsRevisionConflict = errors.New("analytics revision changed")
var ErrAnalyticsForbidden = errors.New("analytics project forbidden")
var ErrAnalyticsInvalidQuery = errors.New("invalid analytics query")
var ErrAnalyticsRebuilding = repository.ErrAnalyticsRebuilding
var ErrAnalyticsIdempotencyConflict = repository.ErrAnalyticsIdempotencyConflict

type AnalyticsService struct{ repo repository.Repository }

func NewAnalyticsService(repo repository.Repository) *AnalyticsService {
	return &AnalyticsService{repo: repo}
}

type AnalyticsObservationInput struct {
	Content     model.AnalyticsContent
	Observation model.AnalyticsObservation
	RawPayload  string
}
type AnalyticsWriteRequest struct {
	ProjectID    string
	Observations []AnalyticsObservationInput
}
type AnalyticsQuery struct {
	From, To, Granularity, MetricBasis   string
	ExpectedRevision                     *int64
	Search, ContentType, Sort, Direction string
	Offset, Limit                        int
}
type AnalyticsMetricMap = map[string]any
type AnalyticsContentView struct {
	model.AnalyticsContent
	Metrics AnalyticsMetricMap `json:"metrics"`
}
type AnalyticsOverview struct {
	Revision    int64                `json:"revision"`
	UpdatedAt   time.Time            `json:"updated_at"`
	MetricBasis string               `json:"metric_basis"`
	Totals      AnalyticsMetricMap   `json:"totals"`
	Series      []AnalyticsMetricMap `json:"series"`
	Coverage    struct {
		Contents int64 `json:"contents"`
	} `json:"coverage"`
	UnavailableMetrics map[string]string `json:"unavailable_metrics"`
}
type AnalyticsContentPage struct {
	Revision int64                  `json:"revision"`
	Items    []AnalyticsContentView `json:"items"`
	Total    int64                  `json:"total"`
	Offset   int                    `json:"offset"`
	Limit    int                    `json:"limit"`
}
type AnalyticsContentDetail struct {
	AnalyticsOverview
	Content AnalyticsContentView `json:"content"`
}
type AnalyticsObservationView struct {
	model.AnalyticsObservation
	Metrics AnalyticsMetricMap `json:"metrics"`
}
type AnalyticsObservationPage struct {
	Revision int64                      `json:"revision"`
	Items    []AnalyticsObservationView `json:"items"`
	Total    int64                      `json:"total"`
	Offset   int                        `json:"offset"`
	Limit    int                        `json:"limit"`
}
type AnalyticsDates struct {
	Revision int64    `json:"revision"`
	Dates    []string `json:"dates"`
}

// LockProjectWrite is intended for existing atomic import/revoke transactions.
func (s *AnalyticsService) LockProjectWrite(ctx context.Context, project string) (int64, error) {
	state, e := s.repo.Analytics().LockProject(ctx, project)
	if e != nil {
		return 0, e
	}
	return state.Revision, nil
}
func (s *AnalyticsService) ClaimIdempotency(ctx context.Context, project, key, hash, batch string) (string, int64, error) {
	if len(key) == 0 || len(key) > 128 || len(hash) != 64 || batch == "" {
		return "", 0, fmt.Errorf("%w: idempotency key, request hash and batch ID required", ErrAnalyticsInvalidQuery)
	}
	return s.repo.Analytics().ClaimIdempotency(ctx, project, key, hash, batch)
}
func validAnalyticsDate(value string) bool {
	t, e := time.ParseInLocation("2006-01-02", value, repository.AnalyticsLocation)
	return e == nil && t.Format("2006-01-02") == value
}
func validateAnalyticsWrite(req AnalyticsWriteRequest) error {
	if req.ProjectID == "" {
		return errors.New("analytics project required")
	}
	if len(req.Observations) > 10000 {
		return errors.New("analytics batch exceeds 10000 observations")
	}
	for _, in := range req.Observations {
		o := in.Observation
		c := in.Content
		if c.ID == "" || len(c.ID) > 100 || c.ProjectID != req.ProjectID || o.ContentID != c.ID || o.ProjectID != req.ProjectID {
			return errors.New("analytics identity mismatch")
		}
		if o.MetricBasis != "daily" && o.MetricBasis != "cumulative" {
			return errors.New("analytics metric_basis must be daily or cumulative")
		}
		if (c.Platform == "article" || c.Platform == "wechat") && o.MetricBasis != "cumulative" {
			return errors.New("WeChat analytics require cumulative basis")
		}
		if !validAnalyticsDate(o.StatDate) || o.EffectiveAt.IsZero() || o.ReceivedAt.IsZero() || o.Source == "" {
			return errors.New("analytics date and provenance required")
		}
		if len(o.ID) > 100 {
			return errors.New("analytics observation identity too long")
		}
		if e := o.AnalyticsMetrics.Validate(); e != nil {
			return e
		}
	}
	return nil
}
func (s *AnalyticsService) Apply(ctx context.Context, req AnalyticsWriteRequest) (int64, error) {
	if e := validateAnalyticsWrite(req); e != nil {
		return 0, e
	}
	var revision int64
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		r := tx.Analytics()
		state, e := r.LockProject(ctx, req.ProjectID)
		if e != nil {
			return e
		}
		revision = state.Revision

		contents := make([]model.AnalyticsContent, 0, len(req.Observations))
		observations := make([]model.AnalyticsObservation, 0, len(req.Observations))
		raw := map[string]string{}
		for _, in := range req.Observations {
			o := in.Observation
			if o.ID == "" {
				o.ID = uuid.NewString()
			}
			contents = append(contents, in.Content)
			observations = append(observations, o)
			raw[o.ID] = in.RawPayload
		}
		keys, e := r.InsertObservations(ctx, contents, observations, raw)
		if e != nil {
			return e
		}
		if e = r.RecomputeDays(ctx, req.ProjectID, state.ActiveGeneration, keys); e != nil {
			return e
		}
		if len(keys) > 0 {
			if e = r.AdvanceRevision(ctx, state); e != nil {
				return e
			}
			revision = state.Revision
		}

		return nil
	})
	return revision, err
}
func (s *AnalyticsService) RevokeBatch(ctx context.Context, project, batch string) (int64, error) {
	if project == "" || batch == "" {
		return 0, errors.New("analytics project and batch required")
	}
	var revision int64
	e := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		r := tx.Analytics()
		state, e := r.LockProject(ctx, project)
		if e != nil {
			return e
		}
		revision = state.Revision
		days, e := r.RevokeBatch(ctx, project, batch)
		if e != nil {
			return e
		}
		if e = r.RecomputeDays(ctx, project, state.ActiveGeneration, days); e != nil {
			return e
		}

		if len(days) > 0 {
			if e = r.AdvanceRevision(ctx, state); e != nil {
				return e
			}
			revision = state.Revision
		}
		return nil
	})
	return revision, e
}
func normalizeAnalyticsQuery(q AnalyticsQuery) (AnalyticsQuery, error) {
	bad := func(msg string) (AnalyticsQuery, error) {
		return q, fmt.Errorf("%w: %s", ErrAnalyticsInvalidQuery, msg)
	}
	if !validAnalyticsDate(q.From) || !validAnalyticsDate(q.To) {
		return bad("from and to must be calendar dates")
	}
	a, _ := time.Parse("2006-01-02", q.From)
	b, _ := time.Parse("2006-01-02", q.To)
	if a.After(b) || b.After(a.AddDate(10, 0, 0)) {
		return bad("range must be ordered and at most ten years")
	}
	if q.MetricBasis != "daily" && q.MetricBasis != "cumulative" {
		return bad("metric_basis required")
	}
	if q.Granularity == "" {
		q.Granularity = "day"
	}
	if q.Granularity != "day" && q.Granularity != "week" && q.Granularity != "month" {
		return bad("invalid granularity")
	}
	if q.Offset < 0 || q.Limit < 0 || q.Limit > 100 {
		return bad("invalid pagination")
	}
	if q.Limit == 0 {
		q.Limit = 25
	}
	if q.Direction != "" && q.Direction != "asc" && q.Direction != "desc" {
		return bad("invalid sort direction")
	}
	if q.Sort != "" && q.Sort != "date" && q.Sort != "title" {
		valid := false
		for _, key := range model.AnalyticsMetricColumns() {
			if q.Sort == key {
				valid = true
			}
		}
		if !valid {
			return bad("invalid sort metric")
		}
	}
	if q.ExpectedRevision != nil && *q.ExpectedRevision < 0 {
		return bad("invalid expected_revision")
	}
	q.Search = strings.TrimSpace(q.Search)
	if len(q.Search) > 500 {
		return bad("search exceeds 500 bytes")
	}
	return q, nil
}
func (s *AnalyticsService) read(ctx context.Context, user, project string, q AnalyticsQuery, fn func(*repository.AnalyticsRepository, *model.Project, *model.AnalyticsState) error) error {
	return s.repo.Analytics().Snapshot(ctx, func(r *repository.AnalyticsRepository) error {
		p, e := r.Project(ctx, project)
		if e != nil {
			return e
		}
		if p.UserID != user {
			return ErrAnalyticsForbidden
		}
		state, e := r.State(ctx, project)
		if e != nil {
			return e
		}
		// During a rebuild the last published generation remains readable; only
		// projects without a published generation are unavailable.
		if state.ActiveGeneration <= 0 {
			return ErrAnalyticsRebuilding
		}
		if q.ExpectedRevision != nil && *q.ExpectedRevision != state.Revision {
			return ErrAnalyticsRevisionConflict
		}
		return fn(r, p, state)
	})
}
func analyticsUnavailable(platform string) map[string]string {
	out := map[string]string{}
	for _, k := range model.AnalyticsDecimalColumns() {
		if model.AnalyticsMetricForPlatform(k, platform) {
			out[k] = "缺少可靠分母，无法汇总比例或平均时长"
		}
	}
	return out
}
func analyticsOverviewRead(ctx context.Context, r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState, q AnalyticsQuery, content string) (AnalyticsOverview, error) {
	out := AnalyticsOverview{Revision: state.Revision, UpdatedAt: state.UpdatedAt, MetricBasis: q.MetricBasis, Series: []AnalyticsMetricMap{}, UnavailableMetrics: analyticsUnavailable(p.Platform)}
	m, n, e := r.RangeTotals(ctx, p.ID, state.ActiveGeneration, q.MetricBasis, q.From, q.To, content, content != "")
	if e != nil {
		return out, e
	}
	out.Totals = m.Map(p.Platform)
	out.Coverage.Contents = n
	if content != "" && q.MetricBasis == "cumulative" {
		out.UnavailableMetrics = map[string]string{}
	}
	rows, e := r.Series(ctx, p.ID, state.ActiveGeneration, q.MetricBasis, q.From, q.To, q.Granularity, content)
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		metrics := row.AnalyticsMetrics
		if content == "" || q.MetricBasis == "daily" {
			metrics.DeliveryCompletionRate = nil
			metrics.ReadCompletionRate = nil
			metrics.AverageReadActiveTime = nil
			metrics.CoverClickRate = nil
			metrics.AvgWatchDuration = nil
		}
		point := metrics.Map(p.Platform)
		point["date"] = row.BucketStart
		out.Series = append(out.Series, point)
	}
	return out, nil
}
func (s *AnalyticsService) Overview(ctx context.Context, user, project string, q AnalyticsQuery) (AnalyticsOverview, error) {
	q, e := normalizeAnalyticsQuery(q)
	if e != nil {
		return AnalyticsOverview{}, e
	}
	var out AnalyticsOverview
	e = s.read(ctx, user, project, q, func(r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState) error {
		var err error
		out, err = analyticsOverviewRead(ctx, r, p, state, q, "")
		return err
	})
	return out, e
}
func (s *AnalyticsService) Contents(ctx context.Context, user, project string, q AnalyticsQuery) (AnalyticsContentPage, error) {
	q, e := normalizeAnalyticsQuery(q)
	if e != nil {
		return AnalyticsContentPage{}, e
	}
	out := AnalyticsContentPage{Items: []AnalyticsContentView{}, Offset: q.Offset, Limit: q.Limit}
	e = s.read(ctx, user, project, q, func(r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState) error {
		out.Revision = state.Revision
		rows, n, e := r.Contents(ctx, project, state.ActiveGeneration, q.MetricBasis, q.From, q.To, repository.AnalyticsContentFilter{Search: q.Search, ContentType: q.ContentType, Sort: q.Sort, Direction: q.Direction, Offset: q.Offset, Limit: q.Limit})
		if e != nil {
			return e
		}
		out.Total = n
		for _, row := range rows {
			out.Items = append(out.Items, AnalyticsContentView{AnalyticsContent: row.AnalyticsContent, Metrics: row.AnalyticsMetrics.Map(p.Platform)})
		}
		return nil
	})
	return out, e
}
func (s *AnalyticsService) Detail(ctx context.Context, user, project, contentID string, q AnalyticsQuery) (AnalyticsContentDetail, error) {
	q, e := normalizeAnalyticsQuery(q)
	if e != nil {
		return AnalyticsContentDetail{}, e
	}
	var out AnalyticsContentDetail
	e = s.read(ctx, user, project, q, func(r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState) error {
		c, e := r.ResolveContent(ctx, project, contentID)
		if e != nil {
			return e
		}
		out.AnalyticsOverview, e = analyticsOverviewRead(ctx, r, p, state, q, c.ID)
		if e != nil {
			return e
		}
		out.Content = AnalyticsContentView{AnalyticsContent: *c, Metrics: out.Totals}
		return nil
	})
	return out, e
}
func (s *AnalyticsService) Observations(ctx context.Context, user, project, contentID string, q AnalyticsQuery) (AnalyticsObservationPage, error) {
	q, e := normalizeAnalyticsQuery(q)
	if e != nil {
		return AnalyticsObservationPage{}, e
	}
	out := AnalyticsObservationPage{Items: []AnalyticsObservationView{}, Offset: q.Offset, Limit: q.Limit}
	e = s.read(ctx, user, project, q, func(r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState) error {
		out.Revision = state.Revision
		c, e := r.ResolveContent(ctx, project, contentID)
		if e != nil {
			return e
		}
		rows, n, e := r.ObservationPage(ctx, project, c.ID, q.From, q.To, q.MetricBasis, q.Offset, q.Limit)
		if e != nil {
			return e
		}
		out.Total = n
		for _, row := range rows {
			out.Items = append(out.Items, AnalyticsObservationView{AnalyticsObservation: row, Metrics: row.AnalyticsMetrics.Map(p.Platform)})
		}
		return nil
	})
	return out, e
}
func (s *AnalyticsService) Dates(ctx context.Context, user, project string, year int, q AnalyticsQuery) (AnalyticsDates, error) {
	if year < 1 || year > 9998 {
		return AnalyticsDates{}, fmt.Errorf("%w: invalid year", ErrAnalyticsInvalidQuery)
	}
	q.From = fmt.Sprintf("%04d-01-01", year)
	q.To = fmt.Sprintf("%04d-12-31", year)
	q, e := normalizeAnalyticsQuery(q)
	if e != nil {
		return AnalyticsDates{}, e
	}
	out := AnalyticsDates{Dates: []string{}}
	e = s.read(ctx, user, project, q, func(r *repository.AnalyticsRepository, p *model.Project, state *model.AnalyticsState) error {
		out.Revision = state.Revision
		var e error
		out.Dates, e = r.Dates(ctx, project, state.ActiveGeneration, q.MetricBasis, year)
		return e
	})
	return out, e
}
