// Package service 中的计费子模块：倍率、额度预扣/回补、用量估算。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// ratioWildcard 倍率兜底模型名。
const ratioWildcard = "*"

// ratioCacheKey 倍率表进程内缓存键。
const ratioCacheKey = "ratios"

// reserveScript 原子预扣：返回 {1, 剩余} / {0, 当前余额} / {-1, 未初始化}。
const reserveScript = `
local key    = KEYS[1]
local amount = tonumber(ARGV[1])
local cur    = redis.call('GET', key)
if not cur then
  return {-1, 0}
end
cur = tonumber(cur)
if cur < amount then
  return {0, cur}
end
local left = redis.call('DECRBY', key, amount)
return {1, left}
`

// settleScript 结算：正差补扣、负差回补，并累计待落库的消耗。
const settleScript = `
local key    = KEYS[1]
local sync   = KEYS[2]
local idx    = KEYS[3]
local diff   = tonumber(ARGV[1])
local actual = tonumber(ARGV[2])
local uid    = ARGV[3]

local cur = tonumber(redis.call('GET', key) or '0')
local charged = 0
if diff > 0 then
  charged = diff
  if cur < charged then charged = cur end
  if charged < 0 then charged = 0 end
  if charged > 0 then redis.call('DECRBY', key, charged) end
elseif diff < 0 then
  charged = diff
  redis.call('INCRBY', key, -diff)
end

if actual > 0 then
  redis.call('INCRBY', sync, actual)
  redis.call('SADD', idx, uid)
  redis.call('EXPIRE', sync, 86400)
end
return {1, charged}
`

// Reservation 一次额度预扣。必须调用 Settle 或 Release 结束。
type Reservation struct {
	// UserID 归属用户。
	UserID uint
	// Reserved 预扣点数。
	Reserved int64

	settled   bool
	container *Container
}

// ReserveQuota 预扣额度，余额不足返回 ErrQuotaExceeded。
func (c *Container) ReserveQuota(ctx context.Context, userID uint, points int64) (*Reservation, error) {
	if userID == 0 {
		return nil, apierr.New(apierr.ErrUnauthorized, "无法确定计费用户")
	}
	if points <= 0 {
		return &Reservation{UserID: userID, container: c}, nil
	}

	key := c.quotaKey(userID)
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := c.scripts.reserve.Run(ctx, c.RDB, []string{key}, points).Result()
		if err != nil {
			return nil, apierr.Wrap(apierr.ErrCache, err)
		}
		values, ok := raw.([]interface{})
		if !ok || len(values) < 2 {
			return nil, apierr.New(apierr.ErrCache, "限流脚本返回异常")
		}
		status, _ := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
		switch status {
		case 1:
			return &Reservation{UserID: userID, Reserved: points, container: c}, nil
		case 0:
			return nil, apierr.Errorf(apierr.ErrQuotaExceeded, "余额不足，需要 %d 点", points)
		default:
			// 计数器未初始化，从数据库加载后重试。
			if err := c.initQuota(ctx, userID); err != nil {
				return nil, err
			}
		}
	}
	return nil, apierr.New(apierr.ErrQuotaExceeded, "额度校验失败，请稍后重试")
}

// Settle 按实际消耗结算：多退少补（回补或补扣）。
func (r *Reservation) Settle(ctx context.Context, actual int64) {
	if r == nil || r.settled {
		return
	}
	r.settled = true
	if r.container == nil {
		return
	}
	r.container.applySettle(ctx, r.UserID, actual-r.Reserved, actual)
}

// Release 全额回补（请求失败或无需计费时使用）。
func (r *Reservation) Release(ctx context.Context) {
	if r == nil || r.settled {
		return
	}
	r.settled = true
	if r.container == nil || r.Reserved <= 0 {
		return
	}
	r.container.applySettle(ctx, r.UserID, -r.Reserved, 0)
}

func (c *Container) applySettle(ctx context.Context, userID uint, diff, actual int64) {
	if diff == 0 && actual == 0 {
		return
	}
	_, err := c.scripts.settle.Run(ctx, c.RDB,
		[]string{c.quotaKey(userID), c.quotaSyncKey(userID), c.quotaSyncIndexKey()},
		diff, actual, strconv.FormatUint(uint64(userID), 10)).Result()
	if err != nil && c.Logger != nil {
		c.Logger.Warn("额度结算失败", zap.Uint("user_id", userID), zap.Error(err))
	}
}

// initQuota 从数据库初始化 Redis 余额计数器。
func (c *Container) initQuota(ctx context.Context, userID uint) error {
	var quota int64
	err := c.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error
	if err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	if err := c.RDB.SetNX(ctx, c.quotaKey(userID), quota, 0).Err(); err != nil {
		return apierr.Wrap(apierr.ErrCache, err)
	}
	return nil
}

// AdjustQuota 管理员调整额度：先落库待结算增量，再改数据库，最后失效缓存。
func (c *Container) AdjustQuota(ctx context.Context, userID uint, delta int64) (*model.User, error) {
	if err := c.flushUserDelta(ctx, userID); err != nil {
		return nil, err
	}

	var user model.User
	err := c.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(gormLock()).Where("id = ?", userID).First(&user).Error; err != nil {
			return apierr.Wrap(apierr.ErrNotFound, err)
		}
		// 不允许把额度调成负数：负余额会让该用户所有调用直接 402 且难以恢复
		if next := user.Quota + delta; next < 0 {
			return apierr.Errorf(apierr.ErrInvalidParam,
				"调整后额度不能为负：当前 %d，本次变更 %d", user.Quota, delta)
		}
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			UpdateColumn("quota", gorm.Expr("quota + ?", delta)).Error; err != nil {
			return apierr.Wrap(apierr.ErrDatabase, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 失效 Redis 计数器，下次访问会重新从数据库加载。
	if err := c.RDB.Del(ctx, c.quotaKey(userID)).Err(); err != nil && c.Logger != nil {
		c.Logger.Warn("失效额度缓存失败", zap.Uint("user_id", userID), zap.Error(err))
	}
	user.Quota += delta
	return &user, nil
}

// FlushQuotaDeltas 把 Redis 中累计的消耗批量落库。
func (c *Container) FlushQuotaDeltas(ctx context.Context) error {
	ids, err := c.RDB.SMembers(ctx, c.quotaSyncIndexKey()).Result()
	if err != nil {
		return apierr.Wrap(apierr.ErrCache, err)
	}
	for _, id := range ids {
		userID, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			_ = c.RDB.SRem(ctx, c.quotaSyncIndexKey(), id).Err()
			continue
		}
		if err := c.flushUserDelta(ctx, uint(userID)); err != nil && c.Logger != nil {
			c.Logger.Warn("额度增量落库失败", zap.Uint("user_id", uint(userID)), zap.Error(err))
		}
	}
	return nil
}

func (c *Container) flushUserDelta(ctx context.Context, userID uint) error {
	syncKey := c.quotaSyncKey(userID)
	value, err := c.RDB.GetDel(ctx, syncKey).Int64()
	if err != nil {
		if err == redis.Nil {
			_ = c.RDB.SRem(ctx, c.quotaSyncIndexKey(), strconv.FormatUint(uint64(userID), 10)).Err()
			return nil
		}
		return apierr.Wrap(apierr.ErrCache, err)
	}
	if value == 0 {
		_ = c.RDB.SRem(ctx, c.quotaSyncIndexKey(), strconv.FormatUint(uint64(userID), 10)).Err()
		return nil
	}

	if err := c.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{
			"quota":      gorm.Expr("quota - ?", value),
			"used_quota": gorm.Expr("used_quota + ?", value),
		}).Error; err != nil {
		// 落库失败：把增量还回去，避免丢账。
		_ = c.RDB.IncrBy(ctx, syncKey, value).Err()
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	_ = c.RDB.SRem(ctx, c.quotaSyncIndexKey(), strconv.FormatUint(uint64(userID), 10)).Err()
	return nil
}

// ---------------------------------------------------------------- 倍率

// RatioFor 返回模型的 prompt / completion 倍率（点 / 1K tokens）。
func (c *Container) RatioFor(ctx context.Context, modelName string) (float64, float64, error) {
	table, err := c.ratioTable(ctx)
	if err != nil {
		return 0, 0, err
	}
	if r, ok := table[modelName]; ok {
		return r.PromptRatio, r.CompletionRatio, nil
	}
	if r, ok := table[ratioWildcard]; ok {
		return r.PromptRatio, r.CompletionRatio, nil
	}
	return c.Config.Gateway.DefaultPromptRatio, c.Config.Gateway.DefaultCompletionRatio, nil
}

// PointsFor 计算用量对应的点数：prompt + completion（含思维链）。
//
// groupMultiplier 为所属分组的倍率系数，在模型倍率算出的点数上相乘：
// 这样分组倍率与按模型的倍率保持正交，管理员只需给分组一个系数。
// 未分组时传 1，行为与加分组功能之前完全一致。
func (c *Container) PointsFor(
	ctx context.Context,
	modelName string,
	usage *spec.Usage,
	groupMultiplier float64,
) (int64, error) {
	if usage == nil {
		return 0, nil
	}
	promptRatio, completionRatio, err := c.RatioFor(ctx, modelName)
	if err != nil {
		return 0, err
	}
	points := float64(usage.PromptTokens)/1000*promptRatio +
		float64(usage.CompletionTokens)/1000*completionRatio
	if points <= 0 {
		return 0, nil
	}
	// 系数在取整前相乘，保证四舍五入只发生一次。
	if groupMultiplier > 0 && groupMultiplier != 1 {
		points *= groupMultiplier
	}
	return int64(math.Ceil(points)), nil
}

// ImageRatioFor 返回模型的生图单价（点 / 张）。
//
// 取值顺序与 RatioFor 保持一致：逐模型 -> 通配符 "*" 兜底 -> 配置默认值。
// 单价为 0 视为"未单独配置"而非"免费"：管理端从兜底价开始编辑，
// 无法区分"管理员填入 0"与"从未配置"，统一按未配置处理；
// 要全局免费应把 gateway.default_image_ratio 配成 0。
func (c *Container) ImageRatioFor(ctx context.Context, modelName string) (float64, error) {
	table, err := c.ratioTable(ctx)
	if err != nil {
		return 0, err
	}
	if r, ok := table[modelName]; ok && r.ImageRatio > 0 {
		return r.ImageRatio, nil
	}
	if r, ok := table[ratioWildcard]; ok && r.ImageRatio > 0 {
		return r.ImageRatio, nil
	}
	return c.Config.Gateway.DefaultImageRatio, nil
}

// VideoRatioFor 返回模型的视频单价（点 / 秒），取值顺序与 ImageRatioFor 一致。
func (c *Container) VideoRatioFor(ctx context.Context, modelName string) (float64, error) {
	table, err := c.ratioTable(ctx)
	if err != nil {
		return 0, err
	}
	if r, ok := table[modelName]; ok && r.VideoRatio > 0 {
		return r.VideoRatio, nil
	}
	if r, ok := table[ratioWildcard]; ok && r.VideoRatio > 0 {
		return r.VideoRatio, nil
	}
	return c.Config.Gateway.DefaultVideoRatio, nil
}

// PointsForVideo 计算视频生成的点数：单价（点 / 秒）× 秒数。
//
// 视频各家计价差异大（有的按条、有的按秒），本网关统一按秒——
// 生成 10 秒和 4 秒的成本差一倍，按条计费会让长视频亏、短视频贵。
func (c *Container) PointsForVideo(
	ctx context.Context,
	modelName string,
	seconds int,
	groupMultiplier float64,
) (int64, error) {
	if seconds <= 0 {
		return 0, nil
	}
	unit, err := c.VideoRatioFor(ctx, modelName)
	if err != nil {
		return 0, err
	}
	if unit <= 0 {
		return 0, nil
	}
	points := unit * float64(seconds)
	if groupMultiplier > 0 && groupMultiplier != 1 {
		points *= groupMultiplier
	}
	return int64(math.Ceil(points)), nil
}

// PointsForImage 计算生图调用的点数：单价（点 / 张）× 张数。
//
// sampleGroupMultiplier 与 token 计费口径一致：分组倍率系数在单价算出的点数上相乘，
// 二者正交；未分组时传 1，行为与加分组功能之前完全一致。
func (c *Container) PointsForImage(
	ctx context.Context,
	modelName string,
	count int,
	groupMultiplier float64,
) (int64, error) {
	if count <= 0 {
		return 0, nil
	}
	unit, err := c.ImageRatioFor(ctx, modelName)
	if err != nil {
		return 0, err
	}
	if unit <= 0 {
		return 0, nil
	}
	points := unit * float64(count)
	if groupMultiplier > 0 && groupMultiplier != 1 {
		points *= groupMultiplier
	}
	return int64(math.Ceil(points)), nil
}

// ratioTable 读取倍率表（进程内缓存 60 秒）。
func (c *Container) ratioTable(ctx context.Context) (map[string]model.ModelRatio, error) {
	if c.KeyCache != nil {
		if v, ok := c.KeyCache.Get(ratioCacheKey); ok {
			if table, ok := v.(map[string]model.ModelRatio); ok {
				return table, nil
			}
		}
	}

	var list []model.ModelRatio
	if err := c.DB.WithContext(ctx).Where("enabled = ?", true).Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	table := make(map[string]model.ModelRatio, len(list))
	for _, r := range list {
		table[r.Model] = r
	}
	if c.KeyCache != nil {
		c.KeyCache.SetWithTTL(ratioCacheKey, table, 60*time.Second)
	}
	return table, nil
}

// invalidateRatioCache 倍率变更后失效缓存。
func (c *Container) invalidateRatioCache() {
	if c.KeyCache != nil {
		c.KeyCache.Delete(ratioCacheKey)
	}
}

// ---------------------------------------------------------------- 倍率 CRUD

// UpsertRatio 新增或更新倍率。
func (c *Container) UpsertRatio(ctx context.Context, r *model.ModelRatio) (*model.ModelRatio, error) {
	if r.Model == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "模型名不能为空")
	}
	if r.PromptRatio < 0 || r.CompletionRatio < 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "倍率不能为负数")
	}
	if r.ImageRatio < 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "生图单价不能为负数")
	}
	if r.VideoRatio < 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "视频单价不能为负数")
	}

	var existing model.ModelRatio
	err := c.DB.WithContext(ctx).Where("model = ?", r.Model).First(&existing).Error
	switch {
	case err == nil:
		if err := c.DB.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"prompt_ratio":     r.PromptRatio,
			"completion_ratio": r.CompletionRatio,
			"image_ratio":      r.ImageRatio,
			"video_ratio":      r.VideoRatio,
			"enabled":          r.Enabled,
		}).Error; err != nil {
			return nil, apierr.Wrap(apierr.ErrDatabase, err)
		}
		existing.PromptRatio = r.PromptRatio
		existing.CompletionRatio = r.CompletionRatio
		existing.ImageRatio = r.ImageRatio
		existing.VideoRatio = r.VideoRatio
		existing.Enabled = r.Enabled
		c.invalidateRatioCache()
		return &existing, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := c.DB.WithContext(ctx).Create(r).Error; err != nil {
			return nil, apierr.Wrap(apierr.ErrDatabase, err)
		}
		c.invalidateRatioCache()
		return r, nil
	default:
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
}

// DeleteRatio 删除倍率。
func (c *Container) DeleteRatio(ctx context.Context, id uint) error {
	res := c.DB.WithContext(ctx).Delete(&model.ModelRatio{}, id)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "倍率配置不存在")
	}
	c.invalidateRatioCache()
	return nil
}

// RatioOverviewItem 倍率配置总览项：渠道里出现的模型 + 其当前生效倍率。
type RatioOverviewItem struct {
	// Model 模型名；"*" 为兜底项。
	Model string `json:"model"`
	// PromptRatio / CompletionRatio 当前生效倍率（点 / 1K tokens）。
	PromptRatio     float64 `json:"prompt_ratio"`
	CompletionRatio float64 `json:"completion_ratio"`
	// Custom 是否已单独配置；false 表示继承兜底倍率。
	Custom bool `json:"custom"`
	// ImageRatio / ImageCustom 生图单价（点 / 张）与其是否已单独配置，含义同 token 倍率。
	ImageRatio  float64 `json:"image_ratio"`
	ImageCustom bool    `json:"image_custom"`
	// VideoRatio / VideoCustom 视频单价（点 / 秒）与其是否已单独配置。
	VideoRatio  float64 `json:"video_ratio"`
	VideoCustom bool    `json:"video_custom"`
	// ID 单独配置的主键；继承项为 0，前端据此判断能否移除。
	ID uint `json:"id"`
	// Enabled 仅对单独配置项有意义：停用后该模型回到兜底计费。
	Enabled bool `json:"enabled"`
	// Channels 提供该模型的渠道名，便于核对来源。
	Channels []string `json:"channels"`
}

// RatioOverview 以渠道模型为基准汇总倍率配置。
//
// 倍率表原本只列出管理员手工添加过的模型，渠道里跑着的模型往往一条都没有，
// 于是「这个模型到底按什么价计费」只能靠猜。这里把两类来源合并：
//
//   - 全部启用渠道对外暴露的模型（用户实际能调用的）
//   - 倍率表里已有的条目（含兜底 * 与已停用项）
//
// 未单独配置的模型回显兜底倍率并标记 Custom=false，
// 管理员直接编辑即可落库为自定义配置。
func (c *Container) RatioOverview(ctx context.Context) ([]RatioOverviewItem, error) {
	var rows []model.ModelRatio
	if err := c.DB.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	byModel := make(map[string]model.ModelRatio, len(rows))
	for _, r := range rows {
		byModel[r.Model] = r
	}

	// 兜底倍率：已启用的 * 行优先，否则回落到配置默认值。
	// 与 RatioFor 的取值顺序保持一致，避免页面显示与实际计费不符。
	fallbackPrompt := c.Config.Gateway.DefaultPromptRatio
	fallbackCompletion := c.Config.Gateway.DefaultCompletionRatio
	if star, ok := byModel[ratioWildcard]; ok && star.Enabled {
		fallbackPrompt, fallbackCompletion = star.PromptRatio, star.CompletionRatio
	}
	// 生图兜底单价：已启用的 * 行优先，否则回落到配置默认值（口径同 ImageRatioFor）。
	fallbackImage := c.Config.Gateway.DefaultImageRatio
	if star, ok := byModel[ratioWildcard]; ok && star.Enabled && star.ImageRatio > 0 {
		fallbackImage = star.ImageRatio
	}
	// 视频兜底单价，口径同 VideoRatioFor。
	fallbackVideo := c.Config.Gateway.DefaultVideoRatio
	if star, ok := byModel[ratioWildcard]; ok && star.Enabled && star.VideoRatio > 0 {
		fallbackVideo = star.VideoRatio
	}

	channels, err := c.EnabledChannels(ctx)
	if err != nil {
		return nil, err
	}
	channelOf := make(map[string][]string)
	for _, ch := range channels {
		for _, name := range ch.ExposedModels() {
			channelOf[name] = append(channelOf[name], ch.Name)
		}
	}

	build := func(name string) RatioOverviewItem {
		item := RatioOverviewItem{
			Model:           name,
			PromptRatio:     fallbackPrompt,
			CompletionRatio: fallbackCompletion,
			ImageRatio:      fallbackImage,
			VideoRatio:      fallbackVideo,
			// 无渠道归属（如兜底项）时给出空数组，避免前端拿到 null
			Channels: append([]string{}, channelOf[name]...),
		}
		if r, ok := byModel[name]; ok {
			item.ID = r.ID
			item.Enabled = r.Enabled
			item.Custom = true
			item.PromptRatio = r.PromptRatio
			item.CompletionRatio = r.CompletionRatio
			// 单价为 0 表示未单独配置（见 ImageRatioFor），此时继续回显兜底价。
			if r.ImageRatio > 0 {
				item.ImageRatio = r.ImageRatio
				item.ImageCustom = true
			}
			if r.VideoRatio > 0 {
				item.VideoRatio = r.VideoRatio
				item.VideoCustom = true
			}
		}
		return item
	}

	out := make([]RatioOverviewItem, 0, len(byModel)+len(channelOf))
	seen := make(map[string]struct{}, cap(out))
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, build(name))
	}

	// 兜底项固定排最前，其余按模型名升序，保证页面顺序稳定
	if _, ok := byModel[ratioWildcard]; ok {
		add(ratioWildcard)
	}
	names := make([]string, 0, len(channelOf))
	for name := range channelOf {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add(name)
	}

	// 仅存在于倍率表、当前没有渠道提供的模型也一并列出：
	// 否则管理员配过的倍率会因为渠道调整而从页面上"消失"，无从清理。
	rest := make([]string, 0, len(byModel))
	for name := range byModel {
		if _, ok := seen[name]; !ok {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name)
	}
	return out, nil
}

// ---------------------------------------------------------------- 估算

// EstimateUsage 上游未返回 usage 时，按请求参数估算用量。
func (c *Container) EstimateUsage(req *spec.UnifiedRequest) *spec.Usage {
	if req == nil {
		return &spec.Usage{}
	}
	var bytes int
	for _, m := range req.Messages {
		bytes += len(m.Content)
		for _, call := range m.ToolCalls {
			bytes += len(call.Name) + len(call.Arguments)
		}
	}
	for _, t := range req.Tools {
		bytes += len(t.Name) + len(t.Description)
	}

	// 粗略折算：约 4 字节 ≈ 1 token（中文按 3 字节/字，结果接近实际）。
	prompt := bytes / 4
	if prompt < 1 {
		prompt = 1
	}

	completion := c.Config.Gateway.EstimateCompletionTokens
	if completion <= 0 {
		completion = 1024
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 && *req.MaxTokens < completion {
		completion = *req.MaxTokens
	}

	return &spec.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
	}
}

// ---------------------------------------------------------------- 键

func (c *Container) quotaKey(userID uint) string {
	return fmt.Sprintf("%s:quota:%d", c.prefix(), userID)
}

func (c *Container) quotaSyncKey(userID uint) string {
	return fmt.Sprintf("%s:qsync:%d", c.prefix(), userID)
}

func (c *Container) quotaSyncIndexKey() string {
	return fmt.Sprintf("%s:qsync:idx", c.prefix())
}
