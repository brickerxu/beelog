package output

import (
	"fmt"
	"strings"

	"github.com/brickerxu/beelog/internal/executor"
)

// diffLineType indicates a diff line's status.
type diffLineType int

const (
	diffSame    diffLineType = iota
	diffRemoved              // present in reference (a), absent in target (b)
	diffAdded                // absent in reference (a), present in target (b)
)

type diffLine struct {
	typ     diffLineType
	content string
}

const diffContextLines = 2

// computeLineDiff produces a line-level diff of aLines vs bLines using LCS.
// Returns nil when either input exceeds 500 lines.
func computeLineDiff(aLines, bLines []string) []diffLine {
	if len(aLines) > 500 || len(bLines) > 500 {
		return nil
	}
	n, m := len(aLines), len(bLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if aLines[i-1] == bLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var result []diffLine
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && aLines[i-1] == bLines[j-1]:
			result = append(result, diffLine{diffSame, aLines[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			result = append(result, diffLine{diffAdded, bLines[j-1]})
			j--
		default:
			result = append(result, diffLine{diffRemoved, aLines[i-1]})
			i--
		}
	}
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}

// anyDiff reports whether diff contains any non-same lines.
func anyDiff(diff []diffLine) bool {
	for _, d := range diff {
		if d.typ != diffSame {
			return true
		}
	}
	return false
}

// formatHunks renders diff as unified-style hunks with context lines.
func formatHunks(diff []diffLine, colorEnabled bool) string {
	const (
		colorDel = "\033[31m"
		colorAdd = "\033[32m"
		colorCtx = "\033[90m"
		reset    = "\033[0m"
	)

	inHunk := make([]bool, len(diff))
	for i, d := range diff {
		if d.typ != diffSame {
			for c := max(0, i-diffContextLines); c <= min(len(diff)-1, i+diffContextLines); c++ {
				inHunk[c] = true
			}
		}
	}

	var sb strings.Builder
	prevInHunk := false
	for i, d := range diff {
		if !inHunk[i] {
			prevInHunk = false
			continue
		}
		if !prevInHunk && i > 0 {
			if colorEnabled {
				sb.WriteString(colorCtx + "  ..." + reset + "\n")
			} else {
				sb.WriteString("  ...\n")
			}
		}
		prevInHunk = true
		if colorEnabled {
			switch d.typ {
			case diffSame:
				sb.WriteString(colorCtx + "  " + d.content + reset + "\n")
			case diffRemoved:
				sb.WriteString(colorDel + "- " + d.content + reset + "\n")
			case diffAdded:
				sb.WriteString(colorAdd + "+ " + d.content + reset + "\n")
			}
		} else {
			switch d.typ {
			case diffSame:
				sb.WriteString("  " + d.content + "\n")
			case diffRemoved:
				sb.WriteString("- " + d.content + "\n")
			case diffAdded:
				sb.WriteString("+ " + d.content + "\n")
			}
		}
	}
	return sb.String()
}

// RenderDiff compares all nodes' outputs and produces a diff report.
// The group with the most nodes is used as the reference baseline.
func RenderDiff(result *executor.BatchResult, colorEnabled bool) string {
	if result == nil || len(result.Results) == 0 {
		return "没有可对比的内容，请先执行一条命令\n"
	}
	if len(result.Results) == 1 {
		return "只有一个节点，无法对比\n"
	}

	const (
		colorGreen = "\033[32m"
		colorRed   = "\033[1;31m"
		reset      = "\033[0m"
	)

	// Group nodes by their trimmed output.
	type group struct {
		output string
		nodes  []string
	}
	seen := make(map[string]int)
	var groups []group
	for _, r := range result.Results {
		out := strings.TrimRight(r.Output, "\n")
		if idx, ok := seen[out]; ok {
			groups[idx].nodes = append(groups[idx].nodes, r.NodeName)
		} else {
			seen[out] = len(groups)
			groups = append(groups, group{output: out, nodes: []string{r.NodeName}})
		}
	}

	if len(groups) == 1 {
		line := fmt.Sprintf("✓ 所有节点输出完全一致 (%d/%d)\n",
			len(result.Results), len(result.Results))
		if colorEnabled {
			return colorGreen + line + reset
		}
		return line
	}

	// Reference = group with the most nodes; ties broken by first occurrence.
	refIdx := 0
	for i := 1; i < len(groups); i++ {
		if len(groups[i].nodes) > len(groups[refIdx].nodes) {
			refIdx = i
		}
	}
	ref := groups[refIdx]
	refLines := splitOutputLines(ref.output)

	var sb strings.Builder
	sameHeader := fmt.Sprintf("✓ 相同 (%d/%d): %s\n",
		len(ref.nodes), len(result.Results), strings.Join(ref.nodes, ", "))
	if colorEnabled {
		sb.WriteString(colorGreen + sameHeader + reset)
	} else {
		sb.WriteString(sameHeader)
	}

	for i, g := range groups {
		if i == refIdx {
			continue
		}
		sb.WriteString("\n")
		diffHeader := fmt.Sprintf("✗ [%s] 差异 (对比 [%s]):\n",
			strings.Join(g.nodes, ", "), ref.nodes[0])
		if colorEnabled {
			sb.WriteString(colorRed + diffHeader + reset)
		} else {
			sb.WriteString(diffHeader)
		}

		diff := computeLineDiff(refLines, splitOutputLines(g.output))
		if diff == nil {
			sb.WriteString("  (输出超过 500 行，省略差异详情)\n")
			continue
		}
		if !anyDiff(diff) {
			sb.WriteString("  (内容相同，仅末尾空白差异)\n")
			continue
		}
		sb.WriteString(formatHunks(diff, colorEnabled))
	}
	return sb.String()
}

// splitOutputLines splits text into lines, stripping the trailing newline.
func splitOutputLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
