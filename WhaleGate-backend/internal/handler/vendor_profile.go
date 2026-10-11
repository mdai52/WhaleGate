package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
	"github.com/whalegate/whalegate/internal/pkg/response"
	"github.com/whalegate/whalegate/internal/service"
)

// VendorProfileHandler 厂商适配档案（管理端）。
type VendorProfileHandler struct {
	svc *service.Container
}

// NewVendorProfileHandler 构造处理器。
func NewVendorProfileHandler(svc *service.Container) *VendorProfileHandler {
	return &VendorProfileHandler{svc: svc}
}

// List 列出全部档案。
func (h *VendorProfileHandler) List(c *gin.Context) {
	rows, err := h.svc.ListVendorProfiles(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, toVendorView(&rows[i]))
	}
	response.OK(c, out)
}

// Get 读取单个档案。
func (h *VendorProfileHandler) Get(c *gin.Context) {
	row, err := h.svc.GetVendorProfile(c.Request.Context(), mustPathID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toVendorView(row))
}

// Create 新建档案。
func (h *VendorProfileHandler) Create(c *gin.Context) {
	in, ok := bindVendorInput(c)
	if !ok {
		return
	}
	row, err := h.svc.CreateVendorProfile(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, toVendorView(row))
}

// Update 更新档案。
func (h *VendorProfileHandler) Update(c *gin.Context) {
	in, ok := bindVendorInput(c)
	if !ok {
		return
	}
	row, err := h.svc.UpdateVendorProfile(c.Request.Context(), mustPathID(c), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toVendorView(row))
}

// Delete 删除档案。
func (h *VendorProfileHandler) Delete(c *gin.Context) {
	if err := h.svc.DeleteVendorProfile(c.Request.Context(), mustPathID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": mustPathID(c), "deleted": true})
}

// Reload 手动重灌内存档案；后台改完一般会自动重载，这里留一个兜底入口。
func (h *VendorProfileHandler) Reload(c *gin.Context) {
	if err := h.svc.LoadVendorProfiles(c.Request.Context()); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"count": len(vendor.All()), "reloaded": true})
}

// Options 返回可选维度，供前端下拉而不是把取值写死在页面上。
//
// 维度取值来自 vendor 包，新增一种风格时前端自动多一个选项。
func (h *VendorProfileHandler) Options(c *gin.Context) {
	response.OK(c, gin.H{
		"reasoning": vendor.ReasoningFields(),
		"usage":     vendor.UsageStyles(),
		"response":  vendor.ResponseStyles(),
		"error":     vendor.ErrorStyles(),
		"auth":      vendor.AuthStyles(),
		"video":     vendor.VideoStyles(),
		"image":     vendor.ImageStyles(),
		"groups":    vendor.Groups(),
	})
}

func bindVendorInput(c *gin.Context) (service.VendorProfileInput, bool) {
	var in service.VendorProfileInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, bindError(err))
		return in, false
	}
	return in, true
}

// toVendorView 组装档案视图：详情以对象形式返回，前端直接编辑。
func toVendorView(row *model.VendorProfile) gin.H {
	return gin.H{
		"id":          row.ID,
		"code":        row.Code,
		"name":        row.Name,
		"group_name":  row.GroupName,
		"match_hosts": row.MatchHosts,
		"enabled":     row.Enabled,
		"sort":        row.Sort,
		"builtin":     row.Builtin,
		"config":      row.Profile(),
		"updated_at":  row.UpdatedAt,
	}
}

func mustPathID(c *gin.Context) uint {
	raw, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	return uint(raw)
}
