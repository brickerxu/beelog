package saver

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/brickerxu/beelog/internal/executor"
	"github.com/brickerxu/beelog/internal/output"
)

// Format 保存格式
type Format string

const (
	FormatText       Format = "text"
	FormatStructured Format = "structured"
	FormatJSON       Format = "json"
	FormatCSV        Format = "csv"
)

// Config 保存配置
type Config struct {
	DefaultDir    string // 默认保存目录，如 ~/beelog_log/
	DefaultFormat Format // 默认格式
}

// record 单行输出记录（内部使用）
type record struct {
	Node      string
	Timestamp time.Time
	Content   string
}

// jsonRecord JSON 序列化结构
type jsonRecord struct {
	Node      string `json:"node"`
	Timestamp string `json:"timestamp"`
	Content   string `json:"content"`
}

// Save 将 BatchResult 保存到文件
// pathArg 为空时自动生成文件名；formatArg 为空时使用配置默认值
// 返回实际写入的文件路径
func Save(result *executor.BatchResult, pathArg string, formatArg string, cfg Config) (string, error) {
	// 解析格式
	format := cfg.DefaultFormat
	if formatArg != "" {
		f := Format(strings.ToLower(formatArg))
		if !isValidFormat(f) {
			return "", fmt.Errorf("不支持的格式 %q，可选: text, structured, json, csv", formatArg)
		}
		format = f
	}
	if !isValidFormat(format) {
		format = FormatText
	}

	// 解析文件路径
	filePath, err := resolvePath(pathArg, cfg.DefaultDir, format)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %w", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	// 处理文件名冲突
	filePath = resolveConflict(filePath)

	// 生成内容
	content, err := render(result, format)
	if err != nil {
		return "", fmt.Errorf("生成内容失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	return filePath, nil
}

func isValidFormat(f Format) bool {
	switch f {
	case FormatText, FormatStructured, FormatJSON, FormatCSV:
		return true
	}
	return false
}

// resolvePath 解析最终文件路径
// - 空 pathArg：自动生成文件名，放在 defaultDir
// - 绝对路径或 ~/：直接使用
// - 含 /：作为相对路径
// - 纯文件名：放在 defaultDir
func resolvePath(pathArg, defaultDir string, format Format) (string, error) {
	expandedDir, err := config.ExpandPath(defaultDir)
	if err != nil {
		return "", err
	}

	if pathArg == "" {
		ts := time.Now().Format("20060102-150405")
		return filepath.Join(expandedDir, fmt.Sprintf("beelog-%s%s", ts, formatExt(format))), nil
	}

	if strings.HasPrefix(pathArg, "~/") || filepath.IsAbs(pathArg) {
		return config.ExpandPath(pathArg)
	}

	if strings.Contains(pathArg, "/") {
		// 相对路径（如 ./result.txt）
		return pathArg, nil
	}

	// 纯文件名
	return filepath.Join(expandedDir, pathArg), nil
}

// resolveConflict 若文件已存在，自动追加 _1, _2... 后缀
func resolveConflict(filePath string) string {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return filePath
	}
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	for i := 1; i <= 999; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext)
}

func formatExt(f Format) string {
	switch f {
	case FormatJSON:
		return ".json"
	case FormatCSV:
		return ".csv"
	default:
		return ".txt"
	}
}

// toRecords 将 BatchResult 拆分为按时间戳稳定排序的记录列表
func toRecords(result *executor.BatchResult) []record {
	var records []record
	for _, r := range result.Results {
		if r.Output == "" {
			continue
		}
		for _, line := range strings.Split(r.Output, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			ts, ok := output.ParseLogTimestamp(line)
			if !ok {
				ts = time.Now()
			}
			records = append(records, record{
				Node:      r.NodeName,
				Timestamp: ts,
				Content:   line,
			})
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	return records
}

func render(result *executor.BatchResult, format Format) (string, error) {
	switch format {
	case FormatText:
		return renderText(result), nil
	case FormatStructured:
		return renderStructured(result), nil
	case FormatJSON:
		return renderJSON(result)
	case FormatCSV:
		return renderCSV(result), nil
	default:
		return "", fmt.Errorf("未知格式: %s", format)
	}
}

func renderText(result *executor.BatchResult) string {
	records := toRecords(result)
	var sb strings.Builder
	for _, r := range records {
		sb.WriteString(r.Content)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func renderStructured(result *executor.BatchResult) string {
	records := toRecords(result)
	var sb strings.Builder
	for _, r := range records {
		fmt.Fprintf(&sb, "[%s] %s  %s\n", r.Node, r.Timestamp.Format("2006-01-02 15:04:05"), r.Content)
	}
	return sb.String()
}

func renderJSON(result *executor.BatchResult) (string, error) {
	records := toRecords(result)
	jRecords := make([]jsonRecord, len(records))
	for i, r := range records {
		jRecords[i] = jsonRecord{
			Node:      r.Node,
			Timestamp: r.Timestamp.Format(time.RFC3339),
			Content:   r.Content,
		}
	}
	data, err := json.MarshalIndent(jRecords, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func renderCSV(result *executor.BatchResult) string {
	records := toRecords(result)
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	_ = w.Write([]string{"node", "timestamp", "content"})
	for _, r := range records {
		_ = w.Write([]string{r.Node, r.Timestamp.Format(time.RFC3339), r.Content})
	}
	w.Flush()
	return sb.String()
}
