package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// MaxDecisionCandidates 是单个来源可交给判断器的候选上限。
const MaxDecisionCandidates = 254

type Manager struct {
	providers []Provider
	ttl       time.Duration
	judge     Judge
}

func NewManager(providers []Provider, ttl time.Duration, judge Judge) *Manager {
	return &Manager{providers: append([]Provider(nil), providers...), ttl: ttl, judge: judge}
}

// Resolve 按来源顺序获取并判断完整事实候选，当前来源确认后立即结束。
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
	supported := false
	for _, p := range m.providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !p.Supports(req.Kind) || req.Ref.Provider != "" && req.Ref.Provider != p.Name() {
			continue
		}
		supported = true
		options, err := m.sourceOptions(ctx, p, req, root)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Name(), err)
		}
		if len(options) == 0 {
			continue
		}
		chosen, err := m.judge.Select(ctx, req, options)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAmbiguous) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if chosen < 0 || chosen >= len(options) {
			return nil, errors.New("判断器返回了候选集合之外的选择")
		}
		return &options[chosen], nil
	}
	if !supported {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnsupported, req.Ref.Provider, req.Kind)
	}
	return nil, ErrNotFound
}

func (m *Manager) sourceOptions(ctx context.Context, p Provider, req Request, root string) ([]Option, error) {
	candidates, err := m.sourceCandidates(ctx, p, req, root)
	if err != nil {
		return nil, err
	}
	if len(candidates) > MaxDecisionCandidates {
		return nil, fmt.Errorf("候选超过 %d 条上限，请缩小作品定位信息", MaxDecisionCandidates)
	}
	options := make([]Option, 0, len(candidates))
	for _, candidate := range candidates {
		work, err := m.fetch(ctx, p, Request{Kind: req.Kind, Ref: candidate.Ref, Group: req.Group}, root)
		if (errors.Is(err, ErrNotFound) || errors.Is(err, ErrConstraintMismatch)) && req.Ref.ID == "" && req.Ref.Slug == "" {
			continue
		}
		if err != nil {
			return nil, err
		}
		option := Option{Work: work}
		if req.Kind == Show && req.Episode > 0 {
			option.Episode, err = m.fetch(ctx, p, Request{Kind: Episode, Ref: work.Ref, Season: req.Season, Episode: req.Episode, Group: req.Group}, root)
			if (errors.Is(err, ErrNotFound) || errors.Is(err, ErrConstraintMismatch)) && req.Ref.ID == "" && req.Ref.Slug == "" {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		options = append(options, option)
	}
	return options, nil
}

func (m *Manager) sourceCandidates(ctx context.Context, p Provider, req Request, root string) ([]Candidate, error) {
	ref := Ref{}
	if req.Ref.Provider == p.Name() && (req.Ref.ID != "" || req.Ref.Slug != "") {
		ref = req.Ref
	} else {
		for _, hint := range req.Hints {
			if hint.Provider != p.Name() {
				continue
			}
			if hint.Kind != req.Kind || hint.ID == "" && hint.Slug == "" {
				return nil, errors.New("LLM 作品引用类型或编号无效")
			}
			if ref.Provider != "" && ref != hint {
				return nil, errors.New("LLM 返回同来源的冲突作品引用")
			}
			ref = hint
		}
	}
	if ref.Provider != "" {
		return []Candidate{{Ref: ref}}, nil
	}
	query := req.Query
	query.Kind = req.Kind
	if strings.TrimSpace(query.Title+query.ChineseTitle+query.OriginalTitle) == "" {
		return nil, errors.New("通用 LLM 未提取可搜索的标题")
	}
	file := cacheFile(root, p, "search", Request{Kind: req.Kind, Query: query})
	candidates, hit, err := readCache[[]Candidate](file, m.ttl)
	if err != nil {
		return nil, err
	}
	if !hit {
		candidates, err = p.Search(ctx, query)
		if err != nil {
			return nil, err
		}
	}
	distinct := make([]Candidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, c := range candidates {
		if c.Ref.Provider != p.Name() || c.Ref.Kind != req.Kind || c.Ref.ID == "" && c.Ref.Slug == "" {
			return nil, errors.New("数据源返回了无效的候选引用")
		}
		identity := c.Ref.ID
		if identity == "" {
			identity = c.Ref.Slug
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		distinct = append(distinct, c)
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
