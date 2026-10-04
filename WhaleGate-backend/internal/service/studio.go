package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/storage"
)

// maxPromptTemplates 词库单次返回上限；词库为人工维护的少量精选模板，无需分页。
const maxPromptTemplates = 200

// maxStoredImageBytes 单张图片落盘上限。
const maxStoredImageBytes = 32 << 20

// ---------------------------------------------------------------- 可选模型

// StudioModel 工作台可选的生图模型。
type StudioModel struct {
	// Model 对外模型名。
	Model string `json:"model"`
	// Channel 承载该模型的渠道名，仅用于提示。
	Channel string `json:"channel"`
}

// ListImageModels 汇总启用渠道中可用于图片生成的模型。
//
// 识别口径：模型目录声明了 image 能力，或模型名命中生图关键词；
// 同时排除不支持图片生成的渠道协议，避免把它们列进下拉框。
func (c *Container) ListImageModels(ctx context.Context) ([]StudioModel, error) {
	channels, err := c.EnabledChannels(ctx)
	if err != nil {
		return nil, err
	}

	capable := make(map[string]struct{})
	var ids []string
	if err := c.DB.WithContext(ctx).Model(&model.ModelCatalog{}).
		Where("capabilities LIKE ?", `%"image"%`).
		Pluck("model_id", &ids).Error; err == nil {
		for _, id := range ids {
			capable[id] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	out := make([]StudioModel, 0, 16)
	for _, ch := range channels {
		if !gateway.SupportsImageGeneration(ch.Type) {
			continue
		}
		for _, name := range ch.ExposedModels() {
			if _, ok := seen[name]; ok {
				continue
			}
			if _, ok := capable[name]; !ok && !gateway.IsImageModel(name) {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, StudioModel{Model: name, Channel: ch.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

// ---------------------------------------------------------------- 词库

// PromptCategoryView 词库分类及其条目数。
type PromptCategoryView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// PromptTemplateView 词库条目视图。
type PromptTemplateView struct {
	ID            uint     `json:"id"`
	Category      string   `json:"category"`
	CategoryLabel string   `json:"category_label"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Prompt        string   `json:"prompt"`
	Tags          []string `json:"tags"`
	ModelHint     string   `json:"model_hint"`
	AspectRatio   string   `json:"aspect_ratio"`
}

// PromptLibrary 词库查询结果：分类统计 + 命中条目。
type PromptLibrary struct {
	Categories []PromptCategoryView `json:"categories"`
	Items      []PromptTemplateView `json:"items"`
}

// ListPromptLibrary 查询提示词词库，category 为空表示全部分类。
func (c *Container) ListPromptLibrary(ctx context.Context, category, keyword string) (*PromptLibrary, error) {
	categories, err := c.promptCategoryCounts(ctx)
	if err != nil {
		return nil, err
	}

	q := c.DB.WithContext(ctx).Model(&model.PromptTemplate{}).Where("enabled = ?", true)
	if category = strings.TrimSpace(category); category != "" {
		q = q.Where("category = ?", category)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("title ILIKE ? OR summary ILIKE ? OR prompt ILIKE ?", like, like, like)
	}

	var rows []model.PromptTemplate
	if err := q.Order("sort DESC, id ASC").Limit(maxPromptTemplates).Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	items := make([]PromptTemplateView, 0, len(rows))
	for _, row := range rows {
		items = append(items, PromptTemplateView{
			ID:            row.ID,
			Category:      row.Category,
			CategoryLabel: model.PromptCategoryLabel(row.Category),
			Title:         row.Title,
			Summary:       row.Summary,
			Prompt:        row.Prompt,
			Tags:          row.Tags,
			ModelHint:     row.ModelHint,
			AspectRatio:   row.AspectRatio,
		})
	}
	return &PromptLibrary{Categories: categories, Items: items}, nil
}

func (c *Container) promptCategoryCounts(ctx context.Context) ([]PromptCategoryView, error) {
	var rows []struct {
		Category string
		Total    int64
	}
	if err := c.DB.WithContext(ctx).Model(&model.PromptTemplate{}).
		Select("category, COUNT(*) AS total").
		Where("enabled = ?", true).
		Group("category").
		Scan(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Category] = row.Total
	}
	// 按预定义顺序输出，保证前端分类顺序稳定；空分类保留但计数为 0。
	out := make([]PromptCategoryView, 0, len(model.PromptCategories))
	for _, def := range model.PromptCategories {
		out = append(out, PromptCategoryView{Key: def.Key, Label: def.Label, Count: counts[def.Key]})
	}
	return out, nil
}

// ---------------------------------------------------------------- 作品库

// ImageWorkInput 工作台发起的一次生成。
type ImageWorkInput struct {
	Model          string
	Prompt         string
	NegativePrompt string
	N              int
	Size           string
	Quality        string
	AspectRatio    string
	TemplateID     uint
	Extra          map[string]interface{}
}

// GeneratedImage 作品中的一张图片。
type GeneratedImage struct {
	// URL 可直接访问的地址：配置了公开访问前缀时为存储直链，否则为本站路径或上游地址。
	URL string `json:"url"`
	// NeedAuth URL 是否需要携带登录态拉取（由网关代理回读时为 true）。
	NeedAuth bool `json:"need_auth"`
	// MimeType 图片类型。
	MimeType string `json:"mime_type"`
	// RevisedPrompt 上游改写后的提示词。
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// withURL 设置图片地址与是否需要鉴权回读。
func (img GeneratedImage) withURL(url string, needAuth bool) GeneratedImage {
	img.URL = url
	img.NeedAuth = needAuth
	return img
}

// ImageWorkView 作品视图。
type ImageWorkView struct {
	ID           uint                   `json:"id"`
	Model        string                 `json:"model"`
	Prompt       string                 `json:"prompt"`
	Params       map[string]interface{} `json:"params"`
	Images       []GeneratedImage       `json:"images"`
	Status       int                    `json:"status"`
	ImageCount   int                    `json:"image_count"`
	LatencyMS    int                    `json:"latency_ms"`
	Source       string                 `json:"source"`
	ChannelID    uint                   `json:"channel_id"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// CreateImageWork 在线生成并落库为一件作品。
// 生成失败不落库，仅向调用方返回错误，避免作品库被失败记录填满。
func (c *Container) CreateImageWork(ctx context.Context, userID uint, in ImageWorkInput, meta *ImageCallMeta) (*ImageWorkView, error) {
	generation, err := c.GenerateImage(ctx, ImageGenerateInput{
		Model:          in.Model,
		Prompt:         in.Prompt,
		NegativePrompt: in.NegativePrompt,
		N:              in.N,
		Size:           in.Size,
		Quality:        in.Quality,
		Extra:          in.Extra,
		User:           strconv.FormatUint(uint64(userID), 10),
		Meta:           meta,
	})
	if err != nil {
		return nil, err
	}

	params := map[string]interface{}{"n": in.N}
	if in.Size != "" {
		params["size"] = in.Size
	}
	if in.Quality != "" {
		params["quality"] = in.Quality
	}
	if in.AspectRatio != "" {
		params["aspect_ratio"] = in.AspectRatio
	}
	if in.TemplateID > 0 {
		params["template_id"] = in.TemplateID
	}

	work := &model.ImageWork{
		UserID:    userID,
		ChannelID: generation.ChannelID,
		Model:     in.Model,
		Prompt:    in.Prompt,
		Params:    params,
		Status:    model.ImageWorkStatusSuccess,
		LatencyMS: int(generation.LatencyMS),
		Source:    "studio",
	}
	if err := c.DB.WithContext(ctx).Create(work).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	work.Images = c.storeImages(ctx, work.ID, generation.Response.Images)
	work.ImageCount = len(work.Images)
	if err := c.DB.WithContext(ctx).Save(work).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	view := c.toImageWorkView(work)
	return &view, nil
}

// ListImageWorks 分页查询当前用户的作品。
func (c *Container) ListImageWorks(ctx context.Context, userID uint, page, pageSize int) ([]ImageWorkView, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.ImageWork{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var rows []model.ImageWork
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	views := make([]ImageWorkView, 0, len(rows))
	for i := range rows {
		views = append(views, c.toImageWorkView(&rows[i]))
	}
	return views, total, nil
}

// DeleteImageWork 删除作品并回收其图片。
func (c *Container) DeleteImageWork(ctx context.Context, userID, workID uint, admin bool) error {
	var work model.ImageWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if err := c.DB.WithContext(ctx).Delete(&model.ImageWork{}, workID).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	// 对象回收失败不阻断删除，仅留日志，避免孤儿对象阻塞用户操作。
	prefix := strconv.FormatUint(uint64(workID), 10)
	if err := c.studioStore.For(assetStorage(work.Images)).Delete(ctx, prefix); err != nil && c.Logger != nil {
		c.Logger.Warn("清理作品图片失败", zap.Uint("work_id", workID), zap.Error(err))
	}
	return nil
}

// WorkImage 一张已打开的作品图片，调用方需关闭 Reader。
type WorkImage struct {
	// Reader 图片内容流。
	Reader io.ReadCloser
	// ContentType 响应类型，如 image/png。
	ContentType string
	// FileName 下载时使用的建议文件名。
	FileName string
}

// ImageWorkFile 校验归属后打开作品图片的读取流。
// 仅当该图片由本服务托管且未配置公开访问地址时才需要走这里；
// 配置了公开地址的会直接由前端直连存储，不占用网关带宽。
func (c *Container) ImageWorkFile(ctx context.Context, userID, workID uint, index int, admin bool) (*WorkImage, error) {
	var work model.ImageWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return nil, apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if index < 0 || index >= len(work.Images) {
		return nil, apierr.New(apierr.ErrNotFound, "图片不存在")
	}

	asset := work.Images[index]
	if !asset.UsesStorage() {
		return nil, apierr.New(apierr.ErrNotFound, "该图片未托管在服务端")
	}
	reader, err := c.studioStore.For(asset.Storage).Open(ctx, asset.Key)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	mime := asset.MimeType
	if mime == "" {
		mime = "image/png"
	}
	return &WorkImage{
		Reader:      reader,
		ContentType: mime,
		FileName:    fmt.Sprintf("whalegate-%d-%d%s", workID, index+1, imageExt(mime)),
	}, nil
}

// assetStorage 取作品图片所用的存储类型：一张作品的图片同批写入，取首个非空的即可。
func assetStorage(assets []model.ImageAsset) string {
	for _, asset := range assets {
		if asset.Storage != "" {
			return asset.Storage
		}
	}
	return storage.KindLocal
}

// toImageWorkView 组装作品视图：已配置公开访问地址的图片给出直链，
// 其余给出需鉴权的服务端地址，由代理回读。
func (c *Container) toImageWorkView(work *model.ImageWork) ImageWorkView {
	images := make([]GeneratedImage, 0, len(work.Images))
	for i, asset := range work.Images {
		img := GeneratedImage{MimeType: asset.MimeType, RevisedPrompt: asset.RevisedPrompt}
		if !asset.UsesStorage() {
			images = append(images, img.withURL(asset.URL, false))
			continue
		}
		if public := c.studioStore.For(asset.Storage).PublicURL(asset.Key); public != "" {
			images = append(images, img.withURL(public, false))
			continue
		}
		images = append(images, img.withURL(
			fmt.Sprintf("/api/v1/studio/works/%d/images/%d", work.ID, i), true))
	}
	return ImageWorkView{
		ID:           work.ID,
		Model:        work.Model,
		Prompt:       work.Prompt,
		Params:       work.Params,
		Images:       images,
		Status:       work.Status,
		ImageCount:   work.ImageCount,
		LatencyMS:    work.LatencyMS,
		Source:       work.Source,
		ChannelID:    work.ChannelID,
		ErrorMessage: work.ErrorMessage,
		CreatedAt:    work.CreatedAt,
	}
}

// ---------------------------------------------------------------- 图片转存

// storeImages 把生成结果写入当前配置的存储后端。单张失败时降级保留上游地址，
// 不影响其余图片落库。
func (c *Container) storeImages(ctx context.Context, workID uint, images []spec.Image) []model.ImageAsset {
	store := c.studioStore.Current()
	assets := make([]model.ImageAsset, 0, len(images))
	for i, img := range images {
		asset, err := c.storeImage(ctx, store, workID, i, img)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn("图片转存失败，降级为上游地址",
					zap.Uint("work_id", workID), zap.Int("index", i), zap.Error(err))
			}
			if img.URL == "" {
				continue
			}
			asset = model.ImageAsset{
				URL: img.URL, MimeType: img.MimeType, RevisedPrompt: img.RevisedPrompt,
			}
		}
		assets = append(assets, asset)
	}
	return assets
}

func (c *Container) storeImage(
	ctx context.Context,
	store storage.Storage,
	workID uint,
	index int,
	img spec.Image,
) (model.ImageAsset, error) {
	data, mime, err := c.fetchImageBytes(ctx, img)
	if err != nil {
		return model.ImageAsset{}, err
	}
	key := storage.JoinKey(strconv.FormatUint(uint64(workID), 10), strconv.Itoa(index)+imageExt(mime))
	if err := store.Put(ctx, storage.Object{Key: key, ContentType: mime, Data: data}); err != nil {
		return model.ImageAsset{}, apierr.Wrap(apierr.ErrInternal, err)
	}
	return model.ImageAsset{
		Storage: store.Kind(), Key: key, MimeType: mime, RevisedPrompt: img.RevisedPrompt,
	}, nil
}

// fetchImageBytes 取得图片原始字节：base64 直接解码，远程地址则下载转存，
// 避免上游临时链接过期后作品库无法回看。
func (c *Container) fetchImageBytes(ctx context.Context, img spec.Image) ([]byte, string, error) {
	if img.B64Data != "" {
		data, err := base64.StdEncoding.DecodeString(img.B64Data)
		if err != nil {
			return nil, "", apierr.Wrap(apierr.ErrUpstreamInvalid, err)
		}
		return data, normalizeImageMime(img.MimeType, data), nil
	}
	if img.URL == "" {
		return nil, "", apierr.New(apierr.ErrUpstreamInvalid, "上游未返回图片内容")
	}

	timeout := c.Config.Studio.DownloadTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, img.URL, nil)
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, "", apierr.Errorf(apierr.ErrUpstream, "转存上游图片失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxStoredImageBytes))
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	mime := img.MimeType
	if mime == "" {
		mime = resp.Header.Get("Content-Type")
	}
	return data, normalizeImageMime(mime, data), nil
}

// normalizeImageMime 收敛图片类型：上游声明优先，缺失时按内容嗅探。
func normalizeImageMime(mime string, data []byte) string {
	mime = strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/avif":
		return mime
	}
	if len(data) > 0 {
		if sniffed := http.DetectContentType(data); strings.HasPrefix(sniffed, "image/") {
			return sniffed
		}
	}
	return "image/png"
}

// imageExt 图片类型对应的文件扩展名，未知类型按 PNG 处理。
func imageExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	default:
		return ".png"
	}
}
