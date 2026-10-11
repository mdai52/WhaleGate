package canvas

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestRenderRectFillsBackground(t *testing.T) {
	scene := Scene{
		Width: 40, Height: 20, Background: "#ffffff",
		Nodes: []Node{{ID: "r1", Type: NodeRect, X: 5, Y: 5, W: 10, H: 10, Fill: "#ff0000"}},
	}
	res, err := Render(scene, Options{Format: FormatPNG, Scale: 1})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if res.Width != 40 || res.Height != 20 {
		t.Fatalf("输出尺寸错误: %dx%d", res.Width, res.Height)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("无法解码输出: %v", err)
	}
	if got := rgbAt(img, 0, 0); got != [3]uint8{255, 255, 255} {
		t.Fatalf("背景应为白色，实际 %v", got)
	}
	if got := rgbAt(img, 10, 10); got != [3]uint8{255, 0, 0} {
		t.Fatalf("矩形内应为红色，实际 %v", got)
	}
}

// 缩放导出应真实放大像素，而不是只改声明尺寸。
func TestRenderScale(t *testing.T) {
	scene := Scene{Width: 10, Height: 10, Background: "#000000"}
	res, err := Render(scene, Options{Format: FormatPNG, Scale: 3})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if res.Width != 30 || res.Height != 30 {
		t.Fatalf("缩放后尺寸错误: %dx%d", res.Width, res.Height)
	}
}

func TestRenderJPEGFlattensTransparent(t *testing.T) {
	// 透明背景 + JPEG：必须铺白，否则透明区会变成黑块。
	scene := Scene{Width: 8, Height: 8, Background: "transparent"}
	res, err := Render(scene, Options{Format: FormatJPEG, Quality: 90})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if len(res.Data) == 0 {
		t.Fatal("JPEG 输出为空")
	}
	if res.MimeType != "image/jpeg" {
		t.Fatalf("MIME 错误: %s", res.MimeType)
	}
}

// 尺寸上限是内存保护的关键，超限必须被拒而不是照做。
func TestRenderRejectsOversized(t *testing.T) {
	limits := Limits{MaxDimension: 100, MaxPixels: 400, MaxNodes: 10, MaxImageBytes: 1 << 20, MaxScale: 2}
	scene := Scene{Width: 200, Height: 200, Background: "#fff"}
	if _, err := Render(scene, Options{Format: FormatPNG, Limits: limits}); err == nil {
		t.Fatal("超过单边上限应报错")
	}
	small := Scene{Width: 50, Height: 50, Background: "#fff"}
	if _, err := Render(small, Options{Format: FormatPNG, Scale: 3, Limits: limits}); err == nil {
		t.Fatal("超过缩放上限应报错")
	}
}

func TestRenderUnknownNodeSkipped(t *testing.T) {
	scene := Scene{
		Width: 10, Height: 10, Background: "#fff",
		Nodes: []Node{{ID: "x", Type: "not-a-shape"}},
	}
	if _, err := Render(scene, Options{Format: FormatPNG}); err != nil {
		t.Fatalf("未知类型应被跳过: %v", err)
	}
}

func TestRenderText(t *testing.T) {
	scene := Scene{
		Width: 200, Height: 60, Background: "#ffffff",
		Nodes: []Node{{ID: "t", Type: NodeText, X: 5, Y: 10, Text: "WhaleGate", FontSize: 20, Color: "#000000"}},
	}
	res, err := Render(scene, Options{Format: FormatPNG})
	if err != nil {
		t.Fatalf("渲染文本失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	// 文本区域应出现非零的深色像素（字形落在左上区域）。
	dark := 0
	for y := 5; y < 40; y++ {
		for x := 5; x < 190; x++ {
			if rgbAt(img, x, y) != [3]uint8{255, 255, 255} {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("文本没有渲染出任何像素")
	}
}

func TestParseColor(t *testing.T) {
	cases := map[string][4]uint8{
		"#f00":            {255, 0, 0, 255},
		"#ff0000":         {255, 0, 0, 255},
		"#ff000080":       {255, 0, 0, 128},
		"rgb(1,2,3)":      {1, 2, 3, 255},
		"rgba(1,2,3,0.5)": {1, 2, 3, 127},
		"transparent":     {0, 0, 0, 0},
		"white":           {255, 255, 255, 255},
		"nonsense":        {0, 0, 0, 0},
	}
	for in, want := range cases {
		got := ParseColor(in)
		if got.R != want[0] || got.G != want[1] || got.B != want[2] || got.A != want[3] {
			t.Errorf("%s 解析为 %v，期望 %v", in, got, want)
		}
	}
}

func TestValidateExport(t *testing.T) {
	if _, _, err := ValidateExport("gif", 1, Limits{}); err == nil {
		t.Fatal("不支持的格式应报错")
	}
	format, scale, err := ValidateExport("", 0, Limits{})
	if err != nil || format != FormatPNG || scale != 1 {
		t.Fatalf("默认应回落 png@1x，实际 %s@%d %v", format, scale, err)
	}
	if format, _, err := ValidateExport("jpg", 1, Limits{}); err != nil || format != FormatJPEG {
		t.Fatalf("jpg 应归一为 jpeg，实际 %s %v", format, err)
	}
}

// 外链图片必须被拒：渲染在服务端执行，任意 URL 会成为 SSRF 入口。
func TestInlineImageOnly(t *testing.T) {
	scene := Scene{
		Width: 10, Height: 10, Background: "#fff",
		Nodes: []Node{{ID: "i", Type: NodeImage, X: 0, Y: 0, Src: "https://evil.example.com/x.png"}},
	}
	if _, err := Render(scene, Options{Format: FormatPNG}); err == nil {
		t.Fatal("外链图片应被拒绝")
	}
}

func rgbAt(img image.Image, x, y int) [3]uint8 {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}
