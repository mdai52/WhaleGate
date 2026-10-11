// 压缩包形式的技能导入：从 zip / tar.gz 里提取所有 SKILL.md。
//
// 全程在内存中解析、不写磁盘，因此不存在 zip slip（路径穿越写文件）风险；
// 但要防压缩炸弹，所以对条目数、单文件与解压总量都设了上限。
//
// 上限由调用方传入（管理员在系统设置里配置），不再写死在编译期：
// 不同部署的技能包规模差异很大，写死会让「上传一个稍大的包」变成无法自助解决的问题。
package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

// Limits 压缩包导入的上限。
type Limits struct {
	// MaxUploadBytes 上传包本身的字节数上限，超过直接拒绝，不必解压。
	MaxUploadBytes int64
	// MaxTotalBytes 解压后的总字节数上限，用于拦截压缩炸弹。
	MaxTotalBytes int64
	// MaxFileBytes 单个文件的字节数上限。
	MaxFileBytes int64
	// MaxEntries 包内条目数上限。
	MaxEntries int
}

// DefaultLimits 未配置时的兜底上限，与历史硬编码保持一致。
func DefaultLimits() Limits {
	return Limits{
		MaxUploadBytes: 32 << 20,
		MaxTotalBytes:  64 << 20,
		MaxFileBytes:   4 << 20,
		MaxEntries:     5000,
	}
}

// normalize 把缺失或非法的上限替换为兜底值，避免 0 值导致「一导入就失败」。
func (l Limits) normalize() Limits {
	def := DefaultLimits()
	if l.MaxUploadBytes <= 0 {
		l.MaxUploadBytes = def.MaxUploadBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = def.MaxTotalBytes
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = def.MaxFileBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = def.MaxEntries
	}
	return l
}

// CheckUpload 在读取上传内容之前先按体积卡一道，避免把大包读进内存。
func (l Limits) CheckUpload(size int64) error {
	if l.MaxUploadBytes > 0 && size > l.MaxUploadBytes {
		return fmt.Errorf("上传文件超过 %d MB（可在系统设置调整）", l.MaxUploadBytes>>20)
	}
	return nil
}

// ParseZip 从 zip 包内容提取所有 SKILL.md。
//
// fallback 用于包根目录下的单个 SKILL.md——此时没有上级目录可取名，
// 退而用包名（通常是仓库名或文件名）。
func ParseZip(data []byte, fallback string, limits Limits) ([]Parsed, error) {
	limits = limits.normalize()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("读取 zip 包失败: %w", err)
	}
	var out []Parsed
	var (
		total   int
		decoded int64
	)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		total++
		if total > limits.MaxEntries {
			return nil, fmt.Errorf("压缩包内文件过多（上限 %d 个）", limits.MaxEntries)
		}
		// zip.File 本身不是 io.Reader，必须先 Open 拿到条目的读取流。
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("打开压缩包内文件失败: %w", err)
		}
		content, err := readArchiveEntry(rc, limits)
		rc.Close()
		if err != nil {
			return nil, err
		}
		decoded += int64(len(content))
		if decoded > limits.MaxTotalBytes {
			return nil, fmt.Errorf("解压后内容超过 %d MB（可在系统设置调整）", limits.MaxTotalBytes>>20)
		}
		out = appendSkill(out, f.Name, content, fallback)
	}
	return out, nil
}

// ParseTarGz 从 tar.gz 包内容提取所有 SKILL.md，fallback 含义同 ParseZip。
func ParseTarGz(data []byte, fallback string, limits Limits) ([]Parsed, error) {
	limits = limits.normalize()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("读取 gzip 包失败: %w", err)
	}
	defer gz.Close()

	var out []Parsed
	var (
		total   int
		decoded int64
	)
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取 tar 包失败: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		total++
		if total > limits.MaxEntries {
			return nil, fmt.Errorf("压缩包内文件过多（上限 %d 个）", limits.MaxEntries)
		}
		content, err := readArchiveEntry(tr, limits)
		if err != nil {
			return nil, err
		}
		decoded += int64(len(content))
		if decoded > limits.MaxTotalBytes {
			return nil, fmt.Errorf("解压后内容超过 %d MB（可在系统设置调整）", limits.MaxTotalBytes>>20)
		}
		out = appendSkill(out, header.Name, content, fallback)
	}
	return out, nil
}

// readArchiveEntry 读取单个条目并施加单文件大小上限。
func readArchiveEntry(r io.Reader, limits Limits) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limits.MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取压缩包内文件失败: %w", err)
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, fmt.Errorf("压缩包内单个文件超过 %d MB（可在系统设置调整）", limits.MaxFileBytes>>20)
	}
	return data, nil
}

// appendSkill 把一个压缩包内文件收进结果；只认 SKILL.md。
func appendSkill(out []Parsed, name string, content []byte, fallback string) []Parsed {
	if !strings.EqualFold(path.Base(strings.ReplaceAll(name, "\\", "/")), "SKILL.md") {
		return out
	}
	parsed := Parse(string(content))
	parsed.FilePath = name
	if parsed.Meta.Name == "" {
		parsed.Meta.Name = archiveSkillName(name, fallback)
	}
	if parsed.Meta.Name == "" {
		return out
	}
	return append(out, parsed)
}

// archiveSkillName 从包内路径推导技能名：取 SKILL.md 的上级目录名
// （skills-main/code-review/SKILL.md → code-review）。
// 包根目录下的 SKILL.md 没有上级目录，退回 fallback（通常是包名）。
func archiveSkillName(name, fallback string) string {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	base := path.Base(path.Dir(clean))
	if base != "." && base != "/" && base != "" {
		return base
	}
	segs := strings.Split(strings.Trim(clean, "/"), "/")
	if len(segs) > 1 {
		return segs[0]
	}
	return strings.TrimSpace(fallback)
}
