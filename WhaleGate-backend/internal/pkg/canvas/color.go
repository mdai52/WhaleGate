package canvas

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// ParseColor 解析 CSS 风格的颜色串，解析不出时返回透明。
//
// 前端给的是 CSS 颜色，而渲染库只认 color.Color，
// 这里统一收口：支持 #RGB / #RRGGBB / #RRGGBBAA / rgb() / rgba() 与少量常用色名。
func ParseColor(s string) color.RGBA {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "transparent" || s == "none" {
		return color.RGBA{}
	}
	if named, ok := namedColors[s]; ok {
		return named
	}
	if strings.HasPrefix(s, "#") {
		return parseHex(s[1:])
	}
	if strings.HasPrefix(s, "rgb") {
		return parseRGBFunc(s)
	}
	return color.RGBA{}
}

func parseHex(hex string) color.RGBA {
	switch len(hex) {
	case 3:
		r := hexDigit(hex[0]) * 17
		g := hexDigit(hex[1]) * 17
		b := hexDigit(hex[2]) * 17
		return color.RGBA{R: r, G: g, B: b, A: 255}
	case 6:
		r := hexDigit(hex[0])<<4 | hexDigit(hex[1])
		g := hexDigit(hex[2])<<4 | hexDigit(hex[3])
		b := hexDigit(hex[4])<<4 | hexDigit(hex[5])
		return color.RGBA{R: r, G: g, B: b, A: 255}
	case 8:
		r := hexDigit(hex[0])<<4 | hexDigit(hex[1])
		g := hexDigit(hex[2])<<4 | hexDigit(hex[3])
		b := hexDigit(hex[4])<<4 | hexDigit(hex[5])
		a := hexDigit(hex[6])<<4 | hexDigit(hex[7])
		return color.RGBA{R: r, G: g, B: b, A: a}
	}
	return color.RGBA{}
}

func hexDigit(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return 0
	}
}

// parseRGBFunc 解析 rgb(1,2,3) / rgba(1,2,3,0.5) 两种写法。
func parseRGBFunc(s string) color.RGBA {
	start := strings.Index(s, "(")
	end := strings.LastIndex(s, ")")
	if start < 0 || end <= start {
		return color.RGBA{}
	}
	parts := strings.Split(s[start+1:end], ",")
	if len(parts) < 3 {
		return color.RGBA{}
	}
	num := func(v string) uint8 {
		v = strings.TrimSpace(v)
		if n, err := strconv.Atoi(v); err == nil {
			return clampByte(n)
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return clampByte(int(f))
		}
		return 0
	}
	out := color.RGBA{R: num(parts[0]), G: num(parts[1]), B: num(parts[2]), A: 255}
	if len(parts) > 3 {
		// alpha 是 0-1 的浮点数，与 rgb 分量不同量纲。
		if f, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64); err == nil {
			out.A = clampByte(int(f * 255))
		}
	}
	return out
}

func clampByte(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}

// namedColors 常用色名兜底：前端拾色器偶尔会给出名字而不是十六进制。
var namedColors = map[string]color.RGBA{
	"white":  {R: 255, G: 255, B: 255, A: 255},
	"black":  {R: 0, G: 0, B: 0, A: 255},
	"red":    {R: 255, G: 0, B: 0, A: 255},
	"green":  {R: 0, G: 128, B: 0, A: 255},
	"blue":   {R: 0, G: 0, B: 255, A: 255},
	"yellow": {R: 255, G: 255, B: 0, A: 255},
	"gray":   {R: 128, G: 128, B: 128, A: 255},
	"grey":   {R: 128, G: 128, B: 128, A: 255},
	"orange": {R: 255, G: 165, B: 0, A: 255},
	"purple": {R: 128, G: 0, B: 128, A: 255},
	"pink":   {R: 255, G: 192, B: 203, A: 255},
	"cyan":   {R: 0, G: 255, B: 255, A: 255},
}

// errUnsupportedImageSrc 图片来源不被接受时的统一报错。
func errUnsupportedImageSrc() error {
	return fmt.Errorf("图片只支持 data URI，外链会在服务端渲染时带来 SSRF 风险")
}
