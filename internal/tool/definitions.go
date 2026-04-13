package tool

type Definition struct {
	Name             string
	Description      string
	PromptSnippet    string
	PromptGuidelines []string
}

var OrderedDefinitions = []Definition{
	{
		Name:             "read",
		Description:      "Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are sent as attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
		PromptSnippet:    "Read file contents",
		PromptGuidelines: []string{"Use read to examine files instead of cat or sed."},
	},
	{
		Name:          "bash",
		Description:   "Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
		PromptSnippet: "Execute bash commands (ls, grep, find, etc.)",
	},
	{
		Name:          "edit",
		Description:   "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes.",
		PromptSnippet: "Make precise file edits with exact text replacement, including multiple disjoint edits in one call",
		PromptGuidelines: []string{
			"Use edit for precise changes (edits[].oldText must match exactly)",
			"When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls",
			"Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.",
			"Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions.",
		},
	},
	{
		Name:             "write",
		Description:      "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
		PromptSnippet:    "Create or overwrite files",
		PromptGuidelines: []string{"Use write only for new files or complete rewrites."},
	},
	{
		Name:          "grep",
		Description:   "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output is truncated to 100 matches or 50KB (whichever is hit first). Long lines are truncated to 500 chars.",
		PromptSnippet: "Search file contents for patterns (respects .gitignore)",
	},
	{
		Name:          "find",
		Description:   "Search for files by glob pattern. Returns matching file paths relative to the search directory. Respects .gitignore. Output is truncated to 1000 results or 50KB (whichever is hit first).",
		PromptSnippet: "Find files by glob pattern (respects .gitignore)",
	},
	{
		Name:          "ls",
		Description:   "List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to 500 entries or 50KB (whichever is hit first).",
		PromptSnippet: "List directory contents",
	},
}

func DefinitionsByName() map[string]Definition {
	out := make(map[string]Definition, len(OrderedDefinitions))
	for _, definition := range OrderedDefinitions {
		out[definition.Name] = definition
	}
	return out
}
