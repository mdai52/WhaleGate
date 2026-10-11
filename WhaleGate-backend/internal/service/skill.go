package service

import (
	"context"
	"path"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/skills"
)

// Skills 缓存：注入发生在每次请求，避免频繁查库。
var (
	skillMu   sync.RWMutex
	skillSnap []model.Skill
	skillAt   time.Time
)

const skillCacheTTL = 30 * time.Second

// ListSkills 分页列出技能。
func (c *Container) ListSkills(ctx context.Context, keyword string, page, pageSize int) ([]model.Skill, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.Skill{})
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + strings.ToLower(kw) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(display_name) LIKE ? OR LOWER(description) LIKE ?", like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.Skill
	if err := q.Order("name ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// GetSkill 读取单个技能。
func (c *Container) GetSkill(ctx context.Context, id uint) (*model.Skill, error) {
	var s model.Skill
	if err := c.DB.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &s, nil
}

// ToggleSkill 启停技能并刷新缓存。
func (c *Container) ToggleSkill(ctx context.Context, id uint, enabled bool) (*model.Skill, error) {
	s, err := c.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled
	if err := c.DB.WithContext(ctx).Save(s).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	invalidateSkillCache()
	return s, nil
}

// SetSkillMode 切换技能的注入模式：全文注入 / 仅索引。
//
// 长技能全文注入会挤占上下文，索引模式只给模型名字与描述、按需再取全文；
// 扫描时按 SKILL.md 的声明写一次，之后管理员应能按实际效果调整。
func (c *Container) SetSkillMode(ctx context.Context, id uint, mode string) (*model.Skill, error) {
	s, err := c.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	switch mode {
	case model.SkillModeAlways, model.SkillModeIndex:
		s.Mode = mode
	default:
		return nil, apierr.Errorf(apierr.ErrInvalidParam,
			"注入模式只能是 %s（全文）或 %s（仅索引）", model.SkillModeAlways, model.SkillModeIndex)
	}
	if err := c.DB.WithContext(ctx).Save(s).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	invalidateSkillCache()
	return s, nil
}

// DeleteSkill 删除技能。
func (c *Container) DeleteSkill(ctx context.Context, id uint) error {
	if err := c.DB.WithContext(ctx).Delete(&model.Skill{}, id).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	invalidateSkillCache()
	return nil
}

// ScanSkills 扫描目录中的 SKILL.md 并写入数据库（按 name 覆盖）。
func (c *Container) ScanSkills(ctx context.Context, dir string) (int, error) {
	if strings.TrimSpace(dir) == "" {
		dir = c.Config.Skills.Dir
	}
	if strings.TrimSpace(dir) == "" {
		return 0, apierr.New(apierr.ErrInvalidParam, "未配置技能目录")
	}
	parsed, err := skills.ScanDir(dir)
	if err != nil {
		return 0, apierr.Wrap(apierr.ErrInternal, err)
	}
	return c.upsertSkills(ctx, parsed, "dir")
}

// ImportSkills 从压缩包内容导入 SKILL.md 并写入数据库（按 name 覆盖）。
//
// 体积与条目上限取自管理员配置（见 SkillsImportLimits），
// 这样「包稍大一点」可以由管理员自助放宽，不必改代码重新发布。
func (c *Container) ImportSkills(ctx context.Context, fileName string, data []byte) (int, error) {
	limits := c.SkillsImportLimits()
	if err := limits.CheckUpload(int64(len(data))); err != nil {
		return 0, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	fallback := archiveBaseName(fileName)

	var (
		parsed []skills.Parsed
		err    error
	)
	switch {
	case strings.HasSuffix(strings.ToLower(fileName), ".zip"):
		parsed, err = skills.ParseZip(data, fallback, limits)
	case strings.HasSuffix(strings.ToLower(fileName), ".tar.gz"),
		strings.HasSuffix(strings.ToLower(fileName), ".tgz"):
		parsed, err = skills.ParseTarGz(data, fallback, limits)
	default:
		return 0, apierr.New(apierr.ErrInvalidParam, "只支持 .zip / .tar.gz / .tgz 压缩包")
	}
	if err != nil {
		return 0, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if len(parsed) == 0 {
		return 0, apierr.New(apierr.ErrInvalidParam, "压缩包内没有找到 SKILL.md")
	}
	return c.upsertSkills(ctx, parsed, "archive")
}

// archiveBaseName 用压缩包文件名作为无上级目录时的技能名兜底。
func archiveBaseName(fileName string) string {
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	for _, suffix := range []string{".tar.gz", ".tgz", ".zip"} {
		if strings.HasSuffix(strings.ToLower(base), suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	return strings.TrimSpace(base)
}

// upsertSkills 把解析结果按 name 覆盖写入数据库。
func (c *Container) upsertSkills(ctx context.Context, parsed []skills.Parsed, source string) (int, error) {
	if len(parsed) == 0 {
		return 0, nil
	}
	now := time.Now()
	rows := make([]model.Skill, 0, len(parsed))
	for _, p := range parsed {
		if p.Meta.Name == "" {
			continue
		}
		mode := p.Meta.Mode
		if mode != model.SkillModeIndex {
			mode = model.SkillModeAlways
		}
		rows = append(rows, model.Skill{
			Name:        p.Meta.Name,
			DisplayName: p.Meta.Name,
			Description: p.Meta.Description,
			Content:     p.Content,
			Mode:        mode,
			Enabled:     true,
			Source:      source,
			FilePath:    p.FilePath,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if len(rows) == 0 {
		return 0, nil
	}

	err := c.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"display_name", "description", "content", "mode", "file_path", "updated_at",
		}),
	}).CreateInBatches(rows, 50).Error
	if err != nil {
		return 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	invalidateSkillCache()
	return len(rows), nil
}

// EnabledSkills 返回启用的技能（带缓存）。
func (c *Container) EnabledSkills(ctx context.Context) []model.Skill {
	skillMu.RLock()
	snap, at := skillSnap, skillAt
	skillMu.RUnlock()

	if len(snap) > 0 && time.Since(at) < skillCacheTTL {
		return snap
	}
	var list []model.Skill
	if err := c.DB.WithContext(ctx).Where("enabled = ?", true).Order("name ASC").Find(&list).Error; err != nil {
		if c.Logger != nil {
			c.Logger.Warn("读取技能列表失败", zap.Error(err))
		}
		return snap
	}
	skillMu.Lock()
	skillSnap, skillAt = list, time.Now()
	skillMu.Unlock()
	return list
}

// BuildSkillPrompt 拼装技能注入文本；无启用技能时返回空串。
func (c *Container) BuildSkillPrompt(ctx context.Context) string {
	list := c.EnabledSkills(ctx)
	if len(list) == 0 {
		return ""
	}
	items := make([]skills.PromptItem, 0, len(list))
	for _, s := range list {
		items = append(items, skills.PromptItem{
			Name:        s.Name,
			DisplayName: s.DisplayName,
			Description: s.Description,
			Content:     s.Content,
			Mode:        s.Mode,
		})
	}
	return skills.BuildPrompt(items)
}

func invalidateSkillCache() {
	skillMu.Lock()
	skillSnap = nil
	skillAt = time.Time{}
	skillMu.Unlock()
}
