// Package skills 解析 SKILL.md：frontmatter 元数据 + 正文，供提示词注入。
package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// Meta SKILL.md 的 frontmatter 元数据。
type Meta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
}

// Parsed 一个解析后的技能文件。
type Parsed struct {
	Meta     Meta
	Content  string
	FilePath string
}

const frontmatterSep = "---"

// Parse 解析 SKILL.md 内容：可选的 YAML frontmatter + 正文。
func Parse(content string) Parsed {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	out := Parsed{Content: strings.TrimSpace(content)}
	if !strings.HasPrefix(content, frontmatterSep) {
		return out
	}
	rest := content[len(frontmatterSep):]
	idx := strings.Index(rest, "\n"+frontmatterSep)
	if idx < 0 {
		return out
	}
	block := rest[:idx]
	body := rest[idx+len("\n"+frontmatterSep):]
	out.Content = strings.TrimSpace(body)
	out.Meta = parseMeta(block)
	return out
}

// parseMeta 解析极简 key: value 形式的 frontmatter（不引入 YAML 依赖）。
func parseMeta(block string) Meta {
	var meta Meta
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		switch key {
		case "name":
			meta.Name = value
		case "description":
			meta.Description = value
		case "mode":
			meta.Mode = value
		}
	}
	return meta
}

// ScanDir 递归扫描目录下所有 SKILL.md。
func ScanDir(root string) ([]Parsed, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	var out []Parsed
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// 跳过隐藏目录与依赖目录
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") ||
				name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		parsed := Parse(string(data))
		parsed.FilePath = path
		if parsed.Meta.Name == "" {
			// 未声明 name 时用上级目录名
			parsed.Meta.Name = filepath.Base(filepath.Dir(path))
		}
		out = append(out, parsed)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BuildPrompt 依据模式拼装注入文本：
// always 模式追加正文，index 模式只追加名称与描述。
func BuildPrompt(items []PromptItem) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# 可用技能\n\n")
	for _, it := range items {
		title := it.DisplayName
		if title == "" {
			title = it.Name
		}
		b.WriteString("## ")
		b.WriteString(title)
		b.WriteString("\n")
		if it.Description != "" {
			b.WriteString(it.Description)
			b.WriteString("\n\n")
		}
		if it.Mode != "index" && it.Content != "" {
			b.WriteString(it.Content)
			b.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// PromptItem 参与拼装的最小信息。
type PromptItem struct {
	Name        string
	DisplayName string
	Description string
	Content     string
	Mode        string
}
