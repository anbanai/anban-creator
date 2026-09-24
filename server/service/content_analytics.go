package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type AnalyticsTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type AnalyticsSelection struct {
	SourceRow int             `json:"source_row"`
	Target    AnalyticsTarget `json:"target"`
}
type AnalyticsCandidate struct {
	Target              AnalyticsTarget          `json:"target"`
	Title               string                   `json:"title"`
	ContentType         string                   `json:"content_type"`
	Status              string                   `json:"status"`
	Date                *time.Time               `json:"date,omitempty"`
	URL                 string                   `json:"url,omitempty"`
	Task                *model.Task              `json:"-"`
	Publication         *model.WechatPublication `json:"-"`
	Post                *model.SeednotePost      `json:"-"`
	aliases             []AnalyticsTarget
	titles              []string
	conflictingIdentity bool
	matchDate           *time.Time
}

type ContentAnalyticsService struct{ repo repository.Repository }

func NewContentAnalyticsService(repo repository.Repository) *ContentAnalyticsService {
	return &ContentAnalyticsService{repo: repo}
}
func (s *ContentAnalyticsService) Candidates(ctx context.Context, userID, projectID, search string, offset, limit int) ([]AnalyticsCandidate, int, error) {
	candidates, err := loadAnalyticsCandidates(ctx, s.repo, userID, projectID)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]AnalyticsCandidate, 0, len(candidates))
	search = strings.ToLower(strings.TrimSpace(search))
	for _, c := range candidates {
		if search == "" || strings.Contains(strings.ToLower(c.Title), search) {
			filtered = append(filtered, c)
		}
	}
	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

func loadAnalyticsCandidates(ctx context.Context, repo repository.Repository, userID, projectID string) ([]AnalyticsCandidate, error) {
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.UserID != userID {
		return nil, errors.New("project does not belong to user")
	}
	if project.Platform != model.PlatformArticle && project.Platform != model.PlatformSeednote {
		return nil, errors.New("project does not support content analytics")
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 0)
	if err != nil {
		return nil, err
	}
	result := make([]AnalyticsCandidate, 0, len(tasks))
	byTask := map[string]int{}
	for _, task := range tasks {
		if task.ProjectID != projectID || task.UserID != userID {
			continue
		}
		title := task.Title
		if title == "" {
			title = task.Prompt
		}
		byTask[task.ID] = len(result)
		contentType := task.Type
		if contentType == model.PlatformSeednote || contentType == "" {
			contentType = "unknown"
		}
		result = append(result, AnalyticsCandidate{Target: AnalyticsTarget{"task", task.ID}, Title: title, ContentType: contentType, Status: task.Status, Date: &task.CreatedAt, Task: task})
	}
	if project.Platform == model.PlatformArticle {
		pubs, err := repo.WechatPublications().ListByProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		for _, pub := range pubs {
			if pub.ProjectID != projectID || pub.UserID != userID {
				continue
			}
			c := AnalyticsCandidate{Target: AnalyticsTarget{"wechat_publication", pub.ID}, Title: pub.DraftTitle, ContentType: "article", Status: pub.Status, Date: pub.PublishedAt, URL: pub.ArticleURL, Publication: pub, matchDate: pub.PublishedAt}
			if i, ok := byTask[pub.TaskID]; ok {
				old := result[i]
				c.Target = old.Target
				c.Task = old.Task
				c.Status = old.Status
				c.aliases = append(old.aliases, AnalyticsTarget{"wechat_publication", pub.ID})
				c.titles = append(old.titles, old.Title)
				if c.Title == "" {
					c.Title = old.Title
				}
				if c.Date == nil {
					c.Date = old.Date
				}
				result[i] = c
			} else {
				result = append(result, c)
			}
		}
	} else {
		posts, total, err := repo.SeednotePosts().ListByProject(ctx, projectID, "", 0, 100)
		if err != nil {
			return nil, err
		}
		if total > int64(len(posts)) {
			posts, _, err = repo.SeednotePosts().ListByProject(ctx, projectID, "", 0, int(total))
			if err != nil {
				return nil, err
			}
		}
		for _, post := range posts {
			if post.ProjectID != projectID || post.UserID != userID {
				continue
			}
			matchDate, matchDateErr := reliableSeednotePostPublishedAt(ctx, repo, post)
			if matchDateErr != nil {
				return nil, matchDateErr
			}
			c := AnalyticsCandidate{Target: AnalyticsTarget{"seednote_post", post.ID}, Title: post.Title, ContentType: post.Genre, Status: "recorded", Date: post.FirstPublishedAt, URL: post.NoteURL, Post: post, matchDate: matchDate}
			if c.ContentType == "" {
				c.ContentType = "unknown"
			}
			if i, ok := byTask[post.TaskID]; ok {
				old := result[i]
				c.Target = old.Target
				c.Task = old.Task
				c.Status = old.Status
				c.aliases = append(old.aliases, AnalyticsTarget{"seednote_post", post.ID})
				c.titles = append(old.titles, old.Title)
				if c.Title == "" {
					c.Title = old.Title
				}
				if c.Date == nil {
					c.Date = old.Date
				}
				result[i] = c
			} else {
				result = append(result, c)
			}
		}
	}
	if project.Platform == model.PlatformArticle {
		for i := range result {
			var snapshots []*model.WechatAnalyticsSnapshot
			if result[i].Task != nil {
				snapshots, err = repo.WechatAnalyticsImports().FindSnapshotsByTaskID(ctx, projectID, result[i].Task.ID)
			}
			if err != nil {
				return nil, err
			}
			if result[i].Publication != nil {
				publicationSnapshots, publicationErr := repo.WechatAnalyticsImports().FindSnapshotsByPublicationID(ctx, projectID, result[i].Publication.ID)
				if publicationErr != nil {
					return nil, publicationErr
				}
				snapshots = append(snapshots, publicationSnapshots...)
			}
			if err != nil {
				return nil, err
			}
			for _, snapshot := range snapshots {
				var raw map[string]string
				if json.Unmarshal([]byte(snapshot.RawData), &raw) == nil && raw["内容标题"] != "" {
					result[i].titles = append(result[i].titles, raw["内容标题"])
				}
			}
			if result[i].URL != "" {
				continue
			}
			result[i].URL = historicalWechatArticleURL(snapshots)
			identities := map[string]struct{}{}
			for _, snapshot := range snapshots {
				var raw map[string]string
				if json.Unmarshal([]byte(snapshot.RawData), &raw) == nil {
					if identity := analyticsPublicIdentity(raw["内容url"]); identity != "" {
						identities[identity] = struct{}{}
					}
				}
			}
			result[i].conflictingIdentity = len(identities) > 1
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Date == nil {
			return false
		}
		if result[j].Date == nil {
			return true
		}
		return result[i].Date.After(*result[j].Date)
	})
	return result, nil
}
func resolveAnalyticsSelections(selections []AnalyticsSelection, candidates []AnalyticsCandidate) (map[int]AnalyticsCandidate, error) {
	if len(selections) == 0 {
		return nil, errors.New("请至少选择一行可导入的数据")
	}
	lookup := map[AnalyticsTarget]AnalyticsCandidate{}
	for _, c := range candidates {
		lookup[c.Target] = c
		for _, a := range c.aliases {
			lookup[a] = c
		}
	}
	result := map[int]AnalyticsCandidate{}
	seen := map[AnalyticsTarget]bool{}
	for _, s := range selections {
		if _, ok := result[s.SourceRow]; ok {
			return nil, fmt.Errorf("第 %d 行被重复选择", s.SourceRow)
		}
		c, ok := lookup[s.Target]
		if !ok {
			return nil, fmt.Errorf("第 %d 行目标不存在或不属于当前账号", s.SourceRow)
		}
		if seen[c.Target] {
			return nil, errors.New("同一内容不能在一个批次中重复导入")
		}
		seen[c.Target] = true
		result[s.SourceRow] = c
	}
	return result, nil
}

// Conflicting nonempty public identities are never rescued by a title match.
func matchAnalyticsCandidate(title string, date *time.Time, link string, candidates []AnalyticsCandidate) (*AnalyticsCandidate, bool) {
	link = analyticsPublicIdentity(link)
	title = normalizeWechatAnalyticsTitle(title)
	unique := func(matches []AnalyticsCandidate) (*AnalyticsCandidate, bool) {
		if len(matches) == 1 {
			return &matches[0], false
		}
		return nil, len(matches) > 1
	}
	if link != "" {
		var matches []AnalyticsCandidate
		for _, c := range candidates {
			if c.URL != "" && analyticsPublicIdentity(c.URL) == link {
				matches = append(matches, c)
			}
		}
		if len(matches) > 0 {
			return unique(matches)
		}
	}
	var titled, dated []AnalyticsCandidate
	for _, c := range candidates {
		if c.conflictingIdentity {
			continue
		}
		if link != "" && c.URL != "" && analyticsPublicIdentity(c.URL) != link {
			continue
		}
		same := normalizeWechatAnalyticsTitle(c.Title) == title
		for _, alias := range c.titles {
			same = same || normalizeWechatAnalyticsTitle(alias) == title
		}
		if !same || title == "" {
			continue
		}
		if date != nil && c.matchDate != nil && c.matchDate.In(date.Location()).Format("2006-01-02") != date.Format("2006-01-02") {
			continue
		}
		titled = append(titled, c)
		if date != nil && c.matchDate != nil && c.matchDate.In(date.Location()).Format("2006-01-02") == date.Format("2006-01-02") {
			dated = append(dated, c)
		}
	}
	if len(dated) > 0 {
		return unique(dated)
	}
	return unique(titled)
}

func analyticsPublicIdentity(value string) string {
	value = normalizeWechatArticleURLForMatch(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	if parsed.Hostname() == "xiaohongshu.com" || strings.HasSuffix(parsed.Hostname(), ".xiaohongshu.com") {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 && (parts[0] == "explore" || parts[0] == "discovery" && parts[1] == "item") {
			return "xiaohongshu:" + parts[len(parts)-1]
		}
	}
	if parsed.Hostname() == "mp.weixin.qq.com" {
		q := parsed.Query()
		if q.Get("__biz") != "" && q.Get("mid") != "" && q.Get("idx") != "" {
			return "wechat:" + q.Get("__biz") + ":" + q.Get("mid") + ":" + q.Get("idx")
		}
		if strings.HasPrefix(parsed.Path, "/s/") && strings.TrimPrefix(parsed.Path, "/s/") != "" {
			parsed.RawQuery = ""
			return parsed.Host + parsed.Path
		}
	}
	return value
}
