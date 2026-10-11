// 画布场景模型与渲染。
//
// 画布工作台把「画了什么」交给前端描述（一组节点），服务端只负责把节点
// 光栅化成 PNG / JPEG。因此本包刻意不含任何数据库与 HTTP 依赖：
// 既方便单测，也让渲染这一 CPU 密集步骤可以被队列单独调度。
package canvas

import (
	"fmt"
	"strings"
)

// 节点类型。
const (
	NodeRect    = "rect"
	NodeEllipse = "ellipse"
	NodeLine    = "line"
	NodeText    = "text"
	NodeImage   = "image"
)

// 导出格式。
const (
	FormatPNG  = "png"
	FormatJPEG = "jpeg"
)

// MimeType 返回导出格式对应的内容类型。
func MimeType(format string) string {
	if format == FormatJPEG {
		return "image/jpeg"
	}
	return "image/png"
}

// Ext 返回导出格式对应的文件扩展名（含点号）。
func Ext(format string) string {
	if format == FormatJPEG {
		return ".jpg"
	}
	return ".png"
}

// Scene 一张画布。
type Scene struct {
	// Width / Height 逻辑尺寸（像素）。
	Width  int `json:"width"`
	Height int `json:"height"`
	// Background 背景色，支持 #RGB / #RRGGBB / #RRGGBBAA / rgb() / rgba()。
	Background string `json:"background"`
	// Nodes 节点列表，按数组顺序绘制（后者盖在前者上）。
	Nodes []Node `json:"nodes"`
}

// Node 画布上的一个元素。
//
// 字段是各类型的并集：不需要的字段为零值即可，
// 这样前端增删属性不必改动协议。
type Node struct {
	ID string `json:"id"`
	// Type 见 Node* 常量。
	Type string `json:"type"`

	// X / Y 左上角坐标；line 类型表示起点。
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// W / H 宽高；line 与 text 可省略。
	W float64 `json:"w,omitempty"`
	H float64 `json:"h,omitempty"`
	// Radius 圆角半径，仅 rect 生效。
	Radius float64 `json:"radius,omitempty"`
	// Rotation 旋转角度（度），绕元素中心旋转。
	Rotation float64 `json:"rotation,omitempty"`
	// Hidden 是否隐藏该节点（隐藏后不绘制，但保留在画布里）。
	Hidden bool `json:"hidden,omitempty"`
	// Opacity 整体不透明度，0-1。
	//
	// 用 0 表示「未设置」而不是「全透明」：omitempty 会让未填写与显式 0 无法区分，
	// 若把 0 当透明，所有没设过透明度的节点都会被静默隐藏。
	Opacity float64 `json:"opacity,omitempty"`

	// Fill 填充色，空字符串表示不填充。
	Fill string `json:"fill,omitempty"`
	// Stroke 描边色，空字符串表示不描边。
	Stroke string `json:"stroke,omitempty"`
	// StrokeWidth 描边宽度（像素）。
	StrokeWidth float64 `json:"stroke_width,omitempty"`

	// Points 折线顶点，仅 line 生效；为空时退化为从 (X,Y) 到 (X+W,Y+H) 的直线。
	Points []Point `json:"points,omitempty"`

	// Text 文本内容，仅 text 生效。
	Text string `json:"text,omitempty"`
	// FontSize 字号（像素）。
	FontSize float64 `json:"font_size,omitempty"`
	// Color 文字颜色；为空时回退 Fill。
	Color string `json:"color,omitempty"`
	// LineHeight 行高倍数，0 或 1 表示默认。
	LineHeight float64 `json:"line_height,omitempty"`

	// Src 图片来源，仅 image 生效。只接受 data URI：
	// 渲染在服务端执行，放开任意 URL 会带来 SSRF 风险。
	Src string `json:"src,omitempty"`
}

// Point 一个坐标点。
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Limits 画布的尺寸与规模上限，防止一次渲染把内存吃满。
type Limits struct {
	// MaxDimension 单边最大像素数。
	MaxDimension int
	// MaxPixels 总像素数上限（宽 × 高 × scale²）。
	MaxPixels int64
	// MaxNodes 节点数上限。
	MaxNodes int
	// MaxImageBytes 单个内嵌图片的字节数上限。
	MaxImageBytes int64
	// MaxScale 导出缩放倍数上限。
	MaxScale int
}

// DefaultLimits 未显式配置时的兜底上限。
//
// 8M 像素的 RGBA 缓冲约 32MB，按默认 4 个渲染 worker 计峰值约 128MB，
// 是可预期的量级；再大就会在导出高峰期把实例内存打满。
func DefaultLimits() Limits {
	return Limits{
		MaxDimension:  4096,
		MaxPixels:     8 << 20,
		MaxNodes:      2000,
		MaxImageBytes: 8 << 20,
		MaxScale:      4,
	}
}

// normalize 用兜底值补齐缺失项，避免零值让所有画布都被拒。
func (l Limits) normalize() Limits {
	def := DefaultLimits()
	if l.MaxDimension <= 0 {
		l.MaxDimension = def.MaxDimension
	}
	if l.MaxPixels <= 0 {
		l.MaxPixels = def.MaxPixels
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = def.MaxNodes
	}
	if l.MaxImageBytes <= 0 {
		l.MaxImageBytes = def.MaxImageBytes
	}
	if l.MaxScale <= 0 {
		l.MaxScale = def.MaxScale
	}
	return l
}

// Validate 校验画布是否在上限之内，返回首个不合规的原因。
func (s Scene) Validate(l Limits) error {
	l = l.normalize()
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("画布尺寸非法")
	}
	if s.Width > l.MaxDimension || s.Height > l.MaxDimension {
		return fmt.Errorf("画布单边不能超过 %d 像素", l.MaxDimension)
	}
	if int64(s.Width)*int64(s.Height) > l.MaxPixels {
		return fmt.Errorf("画布总像素不能超过 %d", l.MaxPixels)
	}
	if len(s.Nodes) > l.MaxNodes {
		return fmt.Errorf("节点数不能超过 %d", l.MaxNodes)
	}
	return nil
}

// ValidateExport 校验导出参数。
func ValidateExport(format string, scale int, l Limits) (string, int, error) {
	l = l.normalize()
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", FormatPNG:
		format = FormatPNG
	case FormatJPEG, "jpg":
		format = FormatJPEG
	default:
		return "", 0, fmt.Errorf("导出格式只支持 png / jpeg")
	}
	if scale <= 0 {
		scale = 1
	}
	if scale > l.MaxScale {
		return "", 0, fmt.Errorf("导出缩放不能超过 %d 倍", l.MaxScale)
	}
	return format, scale, nil
}
