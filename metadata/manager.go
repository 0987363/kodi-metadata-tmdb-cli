package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxDecisionCandidates 是单个来源可交给判断器的候选上限。
const MaxDecisionCandidates = 254

type Manager struct {
	providers []Provider
	ttl       time.Duration
	judge     Judge
	statsMu   sync.Mutex
	decisions map[string]int64
}

func NewManager(providers []Provider, ttl time.Duration, judge Judge) *Manager {
	return &Manager{providers: append([]Provider(nil), providers...), ttl: ttl, judge: judge}
}

// Resolve 先核验作品身份，确认后才获取完整作品与季集事实；任何异常立即返回。
func (m *Manager) Resolve(ctx context.Context, req Request, root string) (*Option, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil || m.judge == nil {
		return nil, errors.New("未配置元数据获取与候选判断器")
	}
	if req.Kind != Movie && req.Kind != Show {
		return nil, errors.New("选择请求必须是电影或节目")
	}
	if req.Ref.Provider != "" && req.Ref.Kind != req.Kind || req.Ref.Provider == "" && (req.Ref.ID != "" || req.Ref.Slug != "") {
		return nil, errors.New("人工作品引用缺少对应来源或对象类型不匹配")
	}
	if req.Episode < 0 || req.Season < 0 || req.Kind == Movie && (req.Episode != 0 || req.Group != "") {
		return nil, errors.New("请求的媒体类型与季集或分组不一致")
	}
	if req.Kind == Show {
		if len(req.Episodes) == 0 {
			return nil, errors.New("剧集请求没有单集集合")
		}
		for _, key := range req.Episodes {
			if key.Season < 0 || key.Episode < 1 {
				return nil, errors.New("剧集请求含未知季集坐标")
			}
		}
	}
	supported := false
	for _, p := range m.providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !p.Supports(req.Kind) || req.Ref.Provider != "" && req.Ref.Provider != p.Name() {
			continue
		}
		supported = true
		fixed := req.Ref.ID != "" || req.Ref.Slug != ""
		var candidates []Candidate
		var manualWork *Record
		var err error
		if fixed {
			manualWork, err = m.fetch(ctx, p, Request{Kind: req.Kind, Ref: req.Ref}, root)
			if err != nil {
				return nil, fmt.Errorf("%s 人工指定作品获取失败: %w: %w", p.Name(), ErrConstraintMismatch, err)
			}
			year := 0
			if len(manualWork.Premiered) >= 4 {
				year, _ = strconv.Atoi(manualWork.Premiered[:4])
			}
			candidates = []Candidate{{Ref: manualWork.Ref, Title: manualWork.Title, OriginalTitle: manualWork.OriginalTitle, Year: year}}
		} else {
			candidates, err = m.sourceCandidates(ctx, p, req, root)
			if err != nil {
				return nil, fmt.Errorf("%s 搜索: %w", p.Name(), err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			continue
		}
		chosen := -1
		if !fixed {
			chosen, err = matchedHint(req, p.Name(), candidates)
			if err != nil {
				return nil, err
			}
		}
		if chosen < 0 {
			m.statsMu.Lock()
			if m.decisions == nil {
				m.decisions = make(map[string]int64)
			}
			m.decisions[p.Name()]++
			m.statsMu.Unlock()
			chosen, err = m.judge.Select(ctx, req, candidates)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if err != nil {
				if fixed {
					return nil, fmt.Errorf("%s 人工指定作品判断失败: %w: %w", p.Name(), ErrConstraintMismatch, err)
				}
				return nil, fmt.Errorf("%s 作品判断失败: %w", p.Name(), err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if chosen < 0 || chosen >= len(candidates) {
			return nil, errors.New("判断器返回了候选集合之外的选择")
		}
		selected := candidates[chosen]
		if req.Kind == Show {
			detail, err := m.fetchSeries(ctx, p, SeriesRequest{Ref: selected.Ref, Episodes: req.Episodes, Details: true}, root)
			if err != nil {
				return nil, fmt.Errorf("%s 选中节目详情不完整: %w", p.Name(), err)
			}
			return &Option{Work: detail.Work, Episodes: detail.Episodes}, nil
		}
		if manualWork != nil {
			return &Option{Work: manualWork}, nil
		}
		work, err := m.fetch(ctx, p, Request{Kind: Movie, Ref: selected.Ref}, root)
		if err != nil {
			return nil, fmt.Errorf("%s 选中电影详情: %w", p.Name(), err)
		}
		return &Option{Work: work}, nil
	}
	if !supported {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnsupported, req.Ref.Provider, req.Kind)
	}
	return nil, ErrNoMatch
}

// matchedHint 只核对当前来源、当前对象类型的作品编号，未命中交给候选判断器。
func matchedHint(req Request, provider string, candidates []Candidate) (int, error) {
	id := ""
	for _, hint := range req.Hints {
		if hint.Provider != provider {
			continue
		}
		if hint.Kind != req.Kind || hint.ID == "" {
			return -1, errors.New("LLM 作品引用类型或编号无效")
		}
		if id != "" && id != hint.ID {
			return -1, errors.New("LLM 返回同来源的冲突作品引用")
		}
		id = hint.ID
	}
	if id == "" {
		return -1, nil
	}
	for i, candidate := range candidates {
		if candidate.Ref.Provider == provider && candidate.Ref.Kind == req.Kind && candidate.Ref.ID == id {
			return i, nil
		}
	}
	return -1, nil
}

func (m *Manager) Statistics() map[string]SourceStats {
	if m == nil {
		return nil
	}
	result := make(map[string]SourceStats, len(m.providers))
	for _, provider := range m.providers {
		stats := SourceStats{}
		if source, ok := provider.(StatisticsProvider); ok {
			stats = source.Statistics()
		}
		result[provider.Name()] = stats
	}
	m.statsMu.Lock()
	for source, count := range m.decisions {
		stats := result[source]
		stats.Decisions = count
		result[source] = stats
	}
	m.statsMu.Unlock()
	return result
}

func (m *Manager) sourceCandidates(ctx context.Context, p Provider, req Request, root string) ([]Candidate, error) {
	query := req.Query
	query.Kind = req.Kind
	if strings.TrimSpace(query.Title+query.ChineseTitle+query.OriginalTitle) == "" {
		return nil, errors.New("通用 LLM 未提取可搜索的标题")
	}
	file := cacheFile(root, p, "identity-search-v1", Request{Kind: req.Kind, Query: query})
	candidates, hit, err := readCache[[]Candidate](file, m.ttl)
	if err != nil {
		return nil, err
	}
	if !hit {
		candidates, err = p.Search(ctx, query)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, ErrNotFound) {
			candidates = nil
			err = nil
		}
		if err != nil {
			return nil, err
		}
	}
	distinct := make([]Candidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.Ref.Provider != p.Name() || candidate.Ref.Kind != req.Kind || strings.TrimSpace(candidate.Ref.ID) == "" || strings.TrimSpace(candidate.Title) == "" {
			return nil, errors.New("数据源返回了无效的候选来源、对象、编号或标题")
		}
		if seen[candidate.Ref.ID] {
			continue
		}
		seen[candidate.Ref.ID] = true
		distinct = append(distinct, candidate)
	}
	if len(distinct) > MaxDecisionCandidates {
		return nil, fmt.Errorf("候选超过 %d 条上限，请缩小作品定位信息", MaxDecisionCandidates)
	}
	if !hit {
		if err := writeCache(file, m.ttl, distinct); err != nil {
			return nil, err
		}
	}
	return distinct, nil
}

func (m *Manager) fetch(ctx context.Context, p Provider, req Request, root string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file := cacheFile(root, p, "detail", req)
	record, hit, err := readCache[*Record](file, m.ttl)
	if err != nil {
		return nil, err
	}
	if !hit {
		record, err = p.Fetch(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	if err := validateRecord(p, req, record); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !hit {
		if err := writeCache(file, m.ttl, record); err != nil {
			return nil, err
		}
	}
	return record, nil
}

func validateRecord(p Provider, req Request, r *Record) error {
	if r == nil || r.Ref.Provider != p.Name() || r.Ref.Kind != req.Kind || strings.TrimSpace(r.Ref.ID) == "" || strings.TrimSpace(r.Title) == "" {
		return errors.New("Provider 返回的来源、对象类型、编号或标题无效")
	}
	if req.Kind != Episode && req.Ref.ID != "" && r.Ref.ID != req.Ref.ID {
		return errors.New("Provider 返回的作品编号与指定编号不一致")
	}
	if req.Kind == Episode && (r.SeasonNumber != req.Season || r.EpisodeNumber != req.Episode) {
		return errors.New("Provider 返回的季集号与请求不一致")
	}
	return nil
}
func LoadReference(file string, kind Kind) (Ref, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return Ref{}, nil
	}
	if err != nil {
		return Ref{}, err
	}
	var ref Ref
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil {
		return Ref{}, fmt.Errorf("读取来源指定文件 %s: %w", file, err)
	}
	if ref.Provider == "" {
		return Ref{}, errors.New("来源指定文件缺少 provider")
	}
	if ref.Kind != "" && ref.Kind != kind {
		return Ref{}, errors.New("来源指定文件对象类型不匹配")
	}
	ref.Kind = kind
	return ref, nil
}
