package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	raw := "---\nname: pdf\ndescription: 处理 PDF 文件\n---\n\n正文内容\n第二行"
	p := Parse(raw)
	if p.Meta.Name != "pdf" || p.Meta.Description != "处理 PDF 文件" {
		t.Fatalf("元数据解析失败: %+v", p.Meta)
	}
	if p.Content != "正文内容\n第二行" {
		t.Errorf("正文解析失败: %q", p.Content)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	p := Parse("只有正文")
	if p.Meta.Name != "" {
		t.Errorf("无 frontmatter 时不应有元数据: %+v", p.Meta)
	}
	if p.Content != "只有正文" {
		t.Errorf("正文异常: %q", p.Content)
	}
}

func TestScanDir(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "pdf"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".hidden"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "pdf", "SKILL.md"),
		[]byte("---\nname: pdf\ndescription: PDF 技能\n---\n正文"), 0o644)
	// 无 name 声明，应回退为目录名
	_ = os.MkdirAll(filepath.Join(root, "excel"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "excel", "SKILL.md"), []byte("表格正文"), 0o644)
	// 应被跳过
	_ = os.WriteFile(filepath.Join(root, ".hidden", "SKILL.md"), []byte("x"), 0o644)

	list, err := ScanDir(root)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个技能，实际 %d", len(list))
	}
	byName := map[string]Parsed{}
	for _, p := range list {
		byName[p.Meta.Name] = p
	}
	if _, ok := byName["pdf"]; !ok {
		t.Error("缺少 pdf 技能")
	}
	if p, ok := byName["excel"]; !ok || p.Content != "表格正文" {
		t.Errorf("excel 技能回退命名或正文异常: %+v", p)
	}
	for _, p := range list {
		if strings.Contains(p.FilePath, ".hidden") {
			t.Error("隐藏目录不应被扫描")
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	items := []PromptItem{
		{Name: "pdf", Description: "PDF 技能", Content: "正文 A", Mode: "always"},
		{Name: "big", Description: "大技能", Content: "很长很长", Mode: "index"},
	}
	out := BuildPrompt(items)
	if !strings.Contains(out, "PDF 技能") || !strings.Contains(out, "正文 A") {
		t.Errorf("always 模式应包含描述与正文: %s", out)
	}
	if strings.Contains(out, "很长很长") {
		t.Errorf("index 模式不应注入正文: %s", out)
	}
	if !strings.Contains(out, "大技能") {
		t.Errorf("index 模式仍应保留描述: %s", out)
	}
}

func TestBuildPromptEmpty(t *testing.T) {
	if BuildPrompt(nil) != "" {
		t.Error("空列表应返回空串")
	}
}
