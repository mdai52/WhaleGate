// Package service 中的计费子模块：倍率、额度预扣/回补、用量估算。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
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
func (c *Container) PointsFor(ctx context.Context, modelName string, usage *spec.Usage) (int64, error) {
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

// ListRatios 分页查询倍率。
func (c *Container) ListRatios(ctx context.Context, page, pageSize int) ([]model.ModelRatio, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.ModelRatio{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var list []model.ModelRatio
	if err := q.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, total, nil
}

// UpsertRatio 新增或更新倍率。
func (c *Container) UpsertRatio(ctx context.Context, r *model.ModelRatio) (*model.ModelRatio, error) {
	if r.Model == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "模型名不能为空")
	}
	if r.PromptRatio < 0 || r.CompletionRatio < 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "倍率不能为负数")
	}

	var existing model.ModelRatio
	err := c.DB.WithContext(ctx).Where("model = ?", r.Model).First(&existing).Error
	switch {
	case err == nil:
		if err := c.DB.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"prompt_ratio":     r.PromptRatio,
			"completion_ratio": r.CompletionRatio,
			"enabled":          r.Enabled,
		}).Error; err != nil {
			return nil, apierr.Wrap(apierr.ErrDatabase, err)
		}
		existing.PromptRatio = r.PromptRatio
		existing.CompletionRatio = r.CompletionRatio
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
