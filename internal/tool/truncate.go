package tool

import (
	"strings"
	"unicode/utf8"
)

type headResult struct {
	Content string
	HeadTruncation
}

type tailResult struct {
	Content string
	TailTruncation
}

func truncateHead(text string, maxLines, maxBytes int) headResult {
	result := headResult{
		Content: text,
		HeadTruncation: HeadTruncation{
			MaxLines: maxLines,
			MaxBytes: maxBytes,
		},
	}
	if text == "" {
		return result
	}

	lines := strings.Split(text, "\n")
	result.TotalLines = len(lines)
	firstLineBytes := len([]byte(lines[0]))
	if firstLineBytes > maxBytes {
		result.Truncated = true
		result.FirstLineExceedsLimit = true
		result.TruncatedBy = "bytes"
		return result
	}

	var builder strings.Builder
	outputLines := 0
	for _, line := range lines {
		candidate := line
		if outputLines > 0 {
			candidate = "\n" + candidate
		}
		if outputLines >= maxLines {
			result.Truncated = true
			result.TruncatedBy = "lines"
			break
		}
		if builder.Len()+len(candidate) > maxBytes {
			result.Truncated = true
			result.TruncatedBy = "bytes"
			break
		}
		builder.WriteString(candidate)
		outputLines++
	}

	if result.Truncated {
		result.Content = builder.String()
		result.OutputLines = outputLines
		result.OutputBytes = len([]byte(result.Content))
		return result
	}

	result.OutputLines = len(lines)
	result.OutputBytes = len([]byte(text))
	return result
}

func truncateTail(text string, maxLines, maxBytes int) tailResult {
	result := tailResult{
		Content: text,
		TailTruncation: TailTruncation{
			MaxLines: maxLines,
			MaxBytes: maxBytes,
		},
	}
	if text == "" {
		return result
	}

	lines := strings.Split(text, "\n")
	result.TotalLines = len(lines)
	if len(lines) > 0 && len([]byte(lines[len(lines)-1])) > maxBytes {
		lastLine := lines[len(lines)-1]
		bytes := []byte(lastLine)
		if len(bytes) > maxBytes {
			bytes = bytes[len(bytes)-maxBytes:]
			for !utf8.Valid(bytes) && len(bytes) > 0 {
				bytes = bytes[1:]
			}
		}
		result.Truncated = true
		result.TruncatedBy = "bytes"
		result.LastLinePartial = true
		result.OutputLines = 1
		result.OutputBytes = len(bytes)
		result.Content = string(bytes)
		return result
	}

	var selected []string
	totalBytes := 0
	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		lineBytes := len([]byte(line))
		addedBytes := lineBytes
		if len(selected) > 0 {
			addedBytes++
		}
		if len(selected) >= maxLines {
			result.Truncated = true
			result.TruncatedBy = "lines"
			break
		}
		if totalBytes+addedBytes > maxBytes {
			result.Truncated = true
			result.TruncatedBy = "bytes"
			break
		}
		selected = append(selected, line)
		totalBytes += addedBytes
	}

	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	result.Content = strings.Join(selected, "\n")
	result.OutputLines = len(selected)
	result.OutputBytes = len([]byte(result.Content))
	return result
}

func truncateLine(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= GrepMaxLineLength {
		return text, false
	}
	return string(runes[:GrepMaxLineLength]) + "...", true
}
