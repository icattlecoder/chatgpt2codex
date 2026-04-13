package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type BashRequest struct {
	Command string `json:"command"`
	Timeout *int   `json:"timeout,omitempty"`
}

type BashDetails struct {
	Truncation     *TailTruncation `json:"truncation,omitempty"`
	FullOutputPath string          `json:"fullOutputPath,omitempty"`
}

func RunBash(ctx context.Context, workspace string, rawBody []byte) (Response, error) {
	var request BashRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Command) == "" {
		return Response{}, ValidationError("command is required")
	}
	if request.Timeout != nil && *request.Timeout <= 0 {
		return Response{}, ValidationError("timeout must be greater than 0")
	}

	commandContext := ctx
	var cancel context.CancelFunc
	if request.Timeout != nil {
		commandContext, cancel = context.WithTimeout(ctx, time.Duration(*request.Timeout)*time.Second)
		defer cancel()
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	command := exec.CommandContext(commandContext, shell, "-lc", request.Command)
	command.Dir = workspace
	output, err := command.CombinedOutput()

	fullOutput := string(output)
	truncation := truncateTail(fullOutput, DefaultMaxLines, DefaultMaxBytes)
	outputText := truncation.Content
	details := BashDetails{}
	if truncation.Truncated {
		tmpFile, fileErr := os.CreateTemp("", "chatgpt2codex-bash-*.log")
		if fileErr != nil {
			return Response{}, ExecutionError(fileErr.Error())
		}
		if _, fileErr = tmpFile.Write(output); fileErr != nil {
			_ = tmpFile.Close()
			return Response{}, ExecutionError(fileErr.Error())
		}
		if fileErr = tmpFile.Close(); fileErr != nil {
			return Response{}, ExecutionError(fileErr.Error())
		}
		details.Truncation = &truncation.TailTruncation
		details.FullOutputPath = tmpFile.Name()
		startLine := truncation.TotalLines - truncation.OutputLines + 1
		endLine := truncation.TotalLines
		switch {
		case truncation.LastLinePartial:
			outputText += fmt.Sprintf("\n\n[Showing last %d bytes of line %d. Full output: %s]", truncation.OutputBytes, endLine, details.FullOutputPath)
		case truncation.TruncatedBy == "lines":
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, truncation.TotalLines, details.FullOutputPath)
		default:
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%d byte limit). Full output: %s]", startLine, endLine, truncation.TotalLines, DefaultMaxBytes, details.FullOutputPath)
		}
	}
	if outputText == "" {
		outputText = "(no output)"
	}

	if err != nil {
		switch {
		case request.Timeout != nil && commandContext.Err() == context.DeadlineExceeded:
			outputText += fmt.Sprintf("\n\nCommand timed out after %d seconds", *request.Timeout)
		case commandContext.Err() == context.Canceled:
			outputText += "\n\nCommand aborted"
		default:
			if exitError, ok := err.(*exec.ExitError); ok {
				outputText += fmt.Sprintf("\n\nCommand exited with code %d", exitError.ExitCode())
			} else {
				outputText += "\n\n" + err.Error()
			}
		}
		response := Response{Content: []ContentItem{{Type: "text", Text: outputText}}}
		if details.Truncation != nil || details.FullOutputPath != "" {
			response.Details = details
		}
		return response, ExecutionError(outputText)
	}

	response := Response{Content: []ContentItem{{Type: "text", Text: outputText}}}
	if details.Truncation != nil || details.FullOutputPath != "" {
		response.Details = details
	}
	return response, nil
}
