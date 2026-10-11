package canvas

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// Options 一次导出的渲染参数。
type Options struct {
	// Format 输出格式：png / jpeg。
	Format string
	// Scale 导出倍数：2 表示按画布尺寸的 2 倍光栅化。
	Scale int
	// Quality JPEG 质量（1-100），PNG 忽略。
	Quality int
	// FontPath 自定义字体文件（TTF/OTF）；为空用内置字体。
	// 内置字体不含中文字形，画布要写中文时由管理员配一个 CJK 字体。
	FontPath string
	// Limits 尺寸与规模上限。
	Limits Limits
}

// Result 渲染结果。
type Result struct {
	// Data 编码后的图像字节。
	Data []byte
	// Width / Height 实际输出尺寸（已计入缩放）。
	Width  int
	Height int
	// MimeType 内容类型。
	MimeType string
}

// bufferPool 复用编码缓冲，避免每次导出都分配一块几 MB 的切片。
var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// 内置字体只需解析一次，之后按字号派生 Face。
var (
	builtinOnce sync.Once
	builtinFont *opentype.Font
	builtinErr  error
)

func loadBuiltinFont() (*opentype.Font, error) {
	builtinOnce.Do(func() {
		builtinFont, builtinErr = opentype.Parse(goregular.TTF)
	})
	return builtinFont, builtinErr
}

// fontFace 取指定字号的字体；配置了字体文件时优先用它。
func fontFace(opts Options, size float64) (font.Face, error) {
	if size <= 0 {
		size = 16
	}
	if p := strings.TrimSpace(opts.FontPath); p != "" {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("读取字体文件失败: %w", err)
		}
		parsed, err := opentype.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("解析字体文件失败: %w", err)
		}
		return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72})
	}
	parsed, err := loadBuiltinFont()
	if err != nil {
		return nil, fmt.Errorf("加载内置字体失败: %w", err)
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72})
}

// Render 把画布光栅化为 PNG / JPEG。
//
// 渲染是 CPU 与内存密集操作：一张 w×h 的画布在内存中是 w*h*4 字节的 RGBA，
// 因此调用方必须先用 Limits 把尺寸卡住（见 Scene.Validate），
// 否则一个「随手填的尺寸」就能吃掉几百 MB。
func Render(scene Scene, opts Options) (*Result, error) {
	limits := opts.Limits.normalize()
	if err := scene.Validate(limits); err != nil {
		return nil, err
	}
	scale := opts.Scale
	if scale <= 0 {
		scale = 1
	}
	if scale > limits.MaxScale {
		return nil, fmt.Errorf("导出缩放不能超过 %d 倍", limits.MaxScale)
	}
	outW, outH := scene.Width*scale, scene.Height*scale
	if int64(outW)*int64(outH) > limits.MaxPixels {
		return nil, fmt.Errorf("导出后总像素不能超过 %d，请降低缩放倍数", limits.MaxPixels)
	}

	dc := gg.NewContext(outW, outH)
	// 先按倍数整体缩放：之后所有坐标都可以用画布逻辑坐标书写，
	// 线宽、字号、圆角会随缩放一起放大，视觉上等同于「高清导出」。
	dc.Scale(float64(scale), float64(scale))

	bg := ParseColor(scene.Background)
	if bg.A == 0 || opts.Format == FormatJPEG {
		// 透明背景在 JPEG 里没有意义（无 alpha 通道），铺白避免导出成黑块。
		bg = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	dc.SetColor(bg)
	dc.Clear()

	for _, node := range scene.Nodes {
		if err := drawNode(dc, node, opts); err != nil {
			return nil, err
		}
	}

	data, err := encode(dc.Image(), opts)
	if err != nil {
		return nil, err
	}
	return &Result{
		Data:     data,
		Width:    outW,
		Height:   outH,
		MimeType: MimeType(opts.Format),
	}, nil
}

// drawNode 绘制单个节点。未知类型直接跳过而不是报错：
// 前端新增类型后，老版本服务端仍能导出已有部分。
func drawNode(dc *gg.Context, n Node, opts Options) error {
	if n.Hidden {
		return nil
	}
	// 0 表示未设置（见 Node.Opacity 注释），按不透明处理。
	opacity := n.Opacity
	if opacity <= 0 || opacity > 1 {
		opacity = 1
	}

	// 旋转围绕元素中心：平移到中心 → 旋转 → 回到左上角坐标系。
	if n.Rotation != 0 {
		dc.Push()
		dc.Translate(n.X+n.W/2, n.Y+n.H/2)
		dc.Rotate(n.Rotation * math.Pi / 180)
		dc.Translate(-n.W/2, -n.H/2)
		defer dc.Pop()
	}

	switch n.Type {
	case NodeRect:
		drawRect(dc, n, opacity)
	case NodeEllipse:
		drawEllipse(dc, n, opacity)
	case NodeLine:
		drawLine(dc, n, opacity)
	case NodeText:
		return drawText(dc, n, opts, opacity)
	case NodeImage:
		return drawImage(dc, n, opts, opacity)
	}
	return nil
}

func drawRect(dc *gg.Context, n Node, opacity float64) {
	if n.W <= 0 || n.H <= 0 {
		return
	}
	radius := n.Radius
	if radius < 0 {
		radius = 0
	}
	if radius > 0 {
		// 圆角不能超过短边的一半，否则形状会自交。
		if limit := math.Min(n.W, n.H) / 2; radius > limit {
			radius = limit
		}
		dc.DrawRoundedRectangle(n.X, n.Y, n.W, n.H, radius)
	} else {
		dc.DrawRectangle(n.X, n.Y, n.W, n.H)
	}
	paint(dc, n, opacity)
}

func drawEllipse(dc *gg.Context, n Node, opacity float64) {
	if n.W <= 0 || n.H <= 0 {
		return
	}
	dc.DrawEllipse(n.X+n.W/2, n.Y+n.H/2, n.W/2, n.H/2)
	paint(dc, n, opacity)
}

func drawLine(dc *gg.Context, n Node, opacity float64) {
	points := n.Points
	if len(points) == 0 {
		// 没有顶点数组时按 (X,Y) → (X+W,Y+H) 画一条直线。
		points = []Point{{X: n.X, Y: n.Y}, {X: n.X + n.W, Y: n.Y + n.H}}
	}
	dc.MoveTo(points[0].X, points[0].Y)
	for _, p := range points[1:] {
		dc.LineTo(p.X, p.Y)
	}
	if fill := withOpacity(ParseColor(n.Fill), opacity); fill.A > 0 {
		// 折线的 Fill 表示自动闭合填充，方便画多边形。
		dc.ClosePath()
		dc.SetColor(fill)
		dc.Fill()
	}
	if stroke := withOpacity(ParseColor(n.Stroke), opacity); stroke.A > 0 {
		dc.SetColor(stroke)
		dc.SetLineWidth(strokeWidth(n))
		dc.Stroke()
	}
}

func drawText(dc *gg.Context, n Node, opts Options, opacity float64) error {
	if strings.TrimSpace(n.Text) == "" {
		return nil
	}
	face, err := fontFace(opts, n.FontSize)
	if err != nil {
		return err
	}
	defer face.Close()
	dc.SetFontFace(face)

	c := withOpacity(ParseColor(n.Color), opacity)
	if c.A == 0 {
		c = withOpacity(ParseColor(n.Fill), opacity)
	}
	if c.A == 0 {
		c = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	}
	dc.SetColor(c)

	lineHeight := n.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1.2
	}
	if n.W > 0 {
		// 给了宽度就按宽度折行，超长文本不会画出画布。
		dc.DrawStringWrapped(n.Text, n.X, n.Y, 0, 0, n.W, lineHeight, gg.AlignLeft)
		return nil
	}
	dc.DrawString(n.Text, n.X, n.Y+float64(face.Metrics().Ascent.Ceil()))
	return nil
}

func drawImage(dc *gg.Context, n Node, opts Options, opacity float64) error {
	if strings.TrimSpace(n.Src) == "" {
		return nil
	}
	img, err := decodeInlineImage(n.Src, opts.Limits.normalize().MaxImageBytes)
	if err != nil {
		return err
	}
	if opacity < 1 {
		img = applyOpacity(img, opacity)
	}
	src := img.Bounds()
	w, h := n.W, n.H
	if w <= 0 || h <= 0 {
		// 未指定尺寸时按图片原始尺寸贴，避免被压成 0 而看不见。
		w, h = float64(src.Dx()), float64(src.Dy())
	}
	if w == float64(src.Dx()) && h == float64(src.Dy()) {
		dc.DrawImage(img, int(math.Round(n.X)), int(math.Round(n.Y)))
	} else {
		// gg 没有带缩放的贴图接口，用仿射变换实现，省掉一次重采样拷贝。
		dc.Push()
		dc.Translate(n.X, n.Y)
		dc.Scale(w/float64(src.Dx()), h/float64(src.Dy()))
		dc.DrawImage(img, 0, 0)
		dc.Pop()
	}
	if stroke := withOpacity(ParseColor(n.Stroke), opacity); stroke.A > 0 {
		dc.DrawRectangle(n.X, n.Y, w, h)
		dc.SetColor(stroke)
		dc.SetLineWidth(strokeWidth(n))
		dc.Stroke()
	}
	return nil
}

// paint 先填充后描边，与浏览器 canvas 的顺序一致。
func paint(dc *gg.Context, n Node, opacity float64) {
	if fill := withOpacity(ParseColor(n.Fill), opacity); fill.A > 0 {
		dc.SetColor(fill)
		dc.Fill()
	}
	if stroke := withOpacity(ParseColor(n.Stroke), opacity); stroke.A > 0 {
		dc.SetColor(stroke)
		dc.SetLineWidth(strokeWidth(n))
		dc.Stroke()
	}
}

// withOpacity 把节点的不透明度折算进颜色分量。
//
// 比起给整段绘制挂一层 mask，直接改颜色更可控，也不依赖渲染库对 mask 的支持范围。
func withOpacity(c color.RGBA, opacity float64) color.RGBA {
	if c.A == 0 || opacity >= 1 {
		return c
	}
	if opacity <= 0 {
		return color.RGBA{}
	}
	c.A = uint8(float64(c.A) * opacity)
	return c
}

// applyOpacity 把图片整体调透明，只在节点设置了半透明时才走这一遍逐像素操作。
func applyOpacity(src image.Image, opacity float64) image.Image {
	bounds := src.Bounds()
	out := image.NewRGBA(bounds)
	factor := uint32(math.Round(opacity * 0xffff))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8),
				A: uint8((a * factor / 0xffff) >> 8),
			})
		}
	}
	return out
}

func strokeWidth(n Node) float64 {
	if n.StrokeWidth > 0 {
		return n.StrokeWidth
	}
	return 1
}

// decodeInlineImage 解析 data URI 形式的内嵌图片。
//
// 不接受外链：渲染跑在服务端，任意 URL 会成为 SSRF 的入口。
func decodeInlineImage(src string, maxBytes int64) (image.Image, error) {
	const prefix = "data:"
	if !strings.HasPrefix(src, prefix) {
		return nil, errUnsupportedImageSrc()
	}
	rest := src[len(prefix):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return nil, errUnsupportedImageSrc()
	}
	meta := rest[:comma]
	payload := rest[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return nil, errUnsupportedImageSrc()
	}
	if int64(len(payload))*3/4 > maxBytes {
		return nil, fmt.Errorf("内嵌图片超过 %d 字节，请压缩后重试", maxBytes)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("解析内嵌图片失败: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("内嵌图片超过 %d 字节，请压缩后重试", maxBytes)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	return img, nil
}

// encode 把渲染结果编码为目标格式。
//
// PNG 用 BestSpeed：默认压缩在大画布上要多花数倍时间，
// 而导出图的体积由存储侧承担，速度对交互体验更重要。
func encode(img image.Image, opts Options) ([]byte, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// 超大缓冲不再回收，避免池里长期驻留一块几十 MB 的内存。
		if buf.Cap() <= 8<<20 {
			bufferPool.Put(buf)
		}
	}()

	if opts.Format == FormatJPEG {
		quality := opts.Quality
		if quality <= 0 {
			quality = 90
		}
		if quality > 100 {
			quality = 100
		}
		// JPEG 没有 alpha：不先合成到白底，透明区会变成黑块。
		if err := jpeg.Encode(buf, flattenToWhite(img), &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("编码 JPEG 失败: %w", err)
		}
	} else {
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(buf, img); err != nil {
			return nil, fmt.Errorf("编码 PNG 失败: %w", err)
		}
	}
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}

// flattenToWhite 把带 alpha 的图像合成到白底，供 JPEG 编码使用。
func flattenToWhite(src image.Image) image.Image {
	bounds := src.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			switch {
			case a >= 0xffff:
				out.Set(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255})
			case a == 0:
				out.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			default:
				// RGBA() 返回的是预乘后的值，先按 alpha 还原再做插值。
				af := float64(a) / 0xffff
				rf := float64(r) / float64(a) * 0xff
				gf := float64(g) / float64(a) * 0xff
				bf := float64(b) / float64(a) * 0xff
				out.Set(x, y, color.RGBA{
					R: uint8(rf*af + 255*(1-af)),
					G: uint8(gf*af + 255*(1-af)),
					B: uint8(bf*af + 255*(1-af)),
					A: 255,
				})
			}
		}
	}
	return out
}
