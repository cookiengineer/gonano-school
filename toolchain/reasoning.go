package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gonano-school/toolchain/parquet"
	"gonano-school/toolchain/types"
)

// The reasoning corpus is DeepSeek-R1 distillation data: questions, a natural
// language reasoning trace, and a final answer. None of DeepSeek's own R1
// training data was released, so the Magpie-Reasoning-V2 set (R1-Llama-70B
// traces) is used as the trace source for the gonano-base corpus.
const (
	ReasoningRepo      = "Magpie-Align/Magpie-Reasoning-V2-250K-CoT-Deepseek-R1-Llama-70B"
	ReasoningConfig    = "default"
	ReasoningSplit     = "train"
	ReasoningSlug      = "magpie-reasoning-v2"
	ReasoningBaseModel = "gonano-base"
)

// ReasoningRecord is one flattened trace.
type ReasoningRecord struct {
	Instruction  string `json:"instruction"`
	Thinking     string `json:"thinking"`
	Response     string `json:"response"`
	Intent       string `json:"intent,omitempty"`
	TaskCategory string `json:"task_category,omitempty"`
	Difficulty   string `json:"difficulty,omitempty"`
	Language     string `json:"language,omitempty"`
	Source       string `json:"source,omitempty"`
}

// ReasoningOptions configures a reasoning ingestion run.
type ReasoningOptions struct {
	Repo           string
	Config         string
	Split          string
	Slug           string
	Category       string // category whose model consumes the traces (default base)
	RootDir        string
	MarkdownDir    string // default <root>/datasets/<category>/markdown/<slug>
	JSONLPath      string // default <root>/datasets/reasoning/<slug>.jsonl
	CacheDir       string // default <root>/datasets/reasoning/<slug>
	Limit          int    // maximum records to emit (0 = all)
	MaxTokens      int    // skip traces longer than this many tokens (0 = no limit)
	RecordsPerFile int    // markdown documents per file
	KeepShards     bool
	DryRun         bool
	HTTPClient     *http.Client
	Logf           func(format string, args ...any)
}

// ReasoningSummary reports what an ingestion run produced.
type ReasoningSummary struct {
	Shards     int
	Records    int
	Complete   int // records with both a reasoning trace and a final answer
	Markdown   []string
	JSONLPath  string
	CacheDir   string
	ShardFiles []string
}

// RunReasoning downloads the configured parquet shards, flattens each row into
// a Markdown document and a JSONL conversation record, and writes both.
func RunReasoning(ctx context.Context, options ReasoningOptions) (ReasoningSummary, error) {
	applyReasoningDefaults(&options)
	summary := ReasoningSummary{JSONLPath: options.JSONLPath, CacheDir: options.CacheDir}

	logfLine(options.Logf, "reasoning: listing shards for %s", options.Repo)
	shards, err := ListReasoningShards(options.Repo, options.Config, options.Split)
	if err != nil {
		return summary, err
	}
	summary.Shards = len(shards)
	logfLine(options.Logf, "reasoning: %d shard(s)", len(shards))

	if options.DryRun {
		for index := range shards {
			logfLine(options.Logf, "would download datasets/reasoning/%s/shard-%05d.parquet", options.Slug, index)
		}
		return summary, nil
	}

	sink, err := newReasoningSink(options)
	if err != nil {
		return summary, err
	}
	defer sink.close()

	// Shards are fetched lazily so a limited run never downloads the whole
	// corpus. Each shard is removed after ingestion unless KeepShards is set.
	for index, url := range shards {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if options.Limit > 0 && summary.Records >= options.Limit {
			break
		}
		item := Item{
			Key: path.Join("datasets", "reasoning", options.Slug, fmt.Sprintf("shard-%05d.parquet", index)),
			Dataset: types.Dataset{
				URL:        url,
				Categories: []string{CategoryBase},
				Model:      ReasoningBaseModel,
			},
		}
		if err := Download(ctx, []Item{item}, DownloadOptions{
			RootDir:    options.RootDir,
			Jobs:       1,
			Retry:      3,
			HTTPClient: options.HTTPClient,
			Logf:       options.Logf,
		}); err != nil {
			return summary, err
		}
		shardPath := filepath.Join(options.RootDir, filepath.FromSlash(item.Key))
		if err := ingestReasoningShard(shardPath, options, sink, &summary); err != nil {
			return summary, err
		}
		if options.KeepShards {
			summary.ShardFiles = append(summary.ShardFiles, shardPath)
		} else {
			os.Remove(shardPath)
		}
	}
	if err := sink.close(); err != nil {
		return summary, err
	}
	summary.Markdown = sink.files
	if !options.KeepShards {
		os.Remove(options.CacheDir)
	}
	logfLine(options.Logf, "reasoning: %d record(s), %d complete, %d markdown file(s)",
		summary.Records, summary.Complete, len(summary.Markdown))
	return summary, nil
}

func applyReasoningDefaults(options *ReasoningOptions) {
	if options.Repo == "" {
		options.Repo = ReasoningRepo
	}
	if options.Config == "" {
		options.Config = ReasoningConfig
	}
	if options.Split == "" {
		options.Split = ReasoningSplit
	}
	if options.Slug == "" {
		options.Slug = ReasoningSlug
	}
	if options.Category == "" {
		options.Category = CategoryBase
	}
	if options.RootDir == "" {
		options.RootDir = "."
	}
	if options.MarkdownDir == "" {
		options.MarkdownDir = filepath.Join(options.RootDir, "datasets", options.Category, "markdown", options.Slug)
	}
	if options.JSONLPath == "" {
		options.JSONLPath = filepath.Join(options.RootDir, "datasets", "reasoning", options.Slug+".jsonl")
	}
	if options.CacheDir == "" {
		options.CacheDir = filepath.Join(options.RootDir, "datasets", "reasoning", options.Slug)
	}
	if options.RecordsPerFile <= 0 {
		options.RecordsPerFile = 1000
	}
}

func ingestReasoningShard(shardPath string, options ReasoningOptions, sink *reasoningSink, summary *ReasoningSummary) error {
	reader, err := parquet.Open(shardPath)
	if err != nil {
		return fmt.Errorf("reasoning: open %s: %w", shardPath, err)
	}
	defer reader.Close()

	switch {
	case reader.HasColumn("response") && reader.HasColumn("instruction"):
		return ingestMagpie(reader, shardPath, options, sink, summary)
	case reader.HasColumn("problem") && reader.HasColumnPath([]string{"generations", "list", "element"}):
		return ingestOpenR1(reader, shardPath, options, sink, summary)
	case reader.HasColumnPath([]string{"messages", "list", "element", "content"}):
		return ingestMixtureOfThoughts(reader, shardPath, options, sink, summary)
	default:
		return fmt.Errorf("reasoning: %s: unrecognized schema (columns: %v)", shardPath, reader.ColumnNames())
	}
}

func ingestMagpie(reader *parquet.Reader, shardPath string, options ReasoningOptions, sink *reasoningSink, summary *ReasoningSummary) error {
	for group := 0; group < reader.NumRowGroups(); group++ {
		if options.Limit > 0 && summary.Records >= options.Limit {
			return nil
		}
		instruction, err := reader.ReadColumnStrings(group, "instruction")
		if err != nil {
			return fmt.Errorf("reasoning: %s: instruction: %w", shardPath, err)
		}
		response, err := reader.ReadColumnStrings(group, "response")
		if err != nil {
			return fmt.Errorf("reasoning: %s: response: %w", shardPath, err)
		}
		intent := optionalColumn(reader, group, "intent", len(instruction))
		taskCategory := optionalColumn(reader, group, "task_category", len(instruction))
		difficulty := optionalColumn(reader, group, "difficulty", len(instruction))
		language := optionalColumn(reader, group, "language", len(instruction))

		for index := 0; index < len(instruction); index++ {
			if options.Limit > 0 && summary.Records >= options.Limit {
				return nil
			}
			if index >= len(response) {
				break
			}
			thinking, answer := ParseReasoningResponse(response[index])
			record := ReasoningRecord{
				Instruction:  strings.TrimSpace(instruction[index]),
				Thinking:     thinking,
				Response:     answer,
				Intent:       fieldAt(intent, index),
				TaskCategory: fieldAt(taskCategory, index),
				Difficulty:   fieldAt(difficulty, index),
				Language:     fieldAt(language, index),
			}
			if err := sink.emit(record, summary); err != nil {
				return err
			}
		}
	}
	return nil
}

// ingestOpenR1 reads the OpenR1-Math-220k schema: a flat `problem`, a flat
// reference `answer`, and a nested `generations` list of R1 traces. The first
// generation marked correct by `correctness_math_verify` is used; otherwise the
// first generation is used.
func ingestOpenR1(reader *parquet.Reader, shardPath string, options ReasoningOptions, sink *reasoningSink, summary *ReasoningSummary) error {
	generationPath := []string{"generations", "list", "element"}
	correctnessPath := []string{"correctness_math_verify", "list", "element"}
	for group := 0; group < reader.NumRowGroups(); group++ {
		if options.Limit > 0 && summary.Records >= options.Limit {
			return nil
		}
		problem, err := reader.ReadColumnStrings(group, "problem")
		if err != nil {
			return fmt.Errorf("reasoning: %s: problem: %w", shardPath, err)
		}
		answer := optionalColumn(reader, group, "answer", len(problem))
		problemType := optionalColumn(reader, group, "problem_type", len(problem))
		generations, err := reader.ReadListStrings(group, generationPath)
		if err != nil {
			return fmt.Errorf("reasoning: %s: generations: %w", shardPath, err)
		}
		correctness, _ := reader.ReadListBools(group, correctnessPath)

		for index := 0; index < len(problem); index++ {
			if options.Limit > 0 && summary.Records >= options.Limit {
				return nil
			}
			if index >= len(generations) || len(generations[index]) == 0 {
				continue
			}
			generationIndex := 0
			if index < len(correctness) {
				for candidate, correct := range correctness[index] {
					if correct && candidate < len(generations[index]) {
						generationIndex = candidate
						break
					}
				}
			}
			thinking, parsedAnswer := ParseReasoningResponse(generations[index][generationIndex])
			response := parsedAnswer
			if response == "" {
				response = fieldAt(answer, index)
			}
			record := ReasoningRecord{
				Instruction:  strings.TrimSpace(problem[index]),
				Thinking:     thinking,
				Response:     strings.TrimSpace(response),
				TaskCategory: fieldAt(problemType, index),
			}
			if err := sink.emit(record, summary); err != nil {
				return err
			}
		}
	}
	return nil
}

// ingestMixtureOfThoughts reads the open-r1/Mixture-of-Thoughts schema: a
// nested `messages` LIST<STRUCT<role, content>> of verified R1 traces, a flat
// `source`, and a flat `num_tokens` used to skip traces that exceed MaxTokens.
func ingestMixtureOfThoughts(reader *parquet.Reader, shardPath string, options ReasoningOptions, sink *reasoningSink, summary *ReasoningSummary) error {
	contentPath := []string{"messages", "list", "element", "content"}
	rolePath := []string{"messages", "list", "element", "role"}
	for group := 0; group < reader.NumRowGroups(); group++ {
		if options.Limit > 0 && summary.Records >= options.Limit {
			return nil
		}
		contents, err := reader.ReadListStrings(group, contentPath)
		if err != nil {
			return fmt.Errorf("reasoning: %s: messages.content: %w", shardPath, err)
		}
		roles, err := reader.ReadListStrings(group, rolePath)
		if err != nil {
			return fmt.Errorf("reasoning: %s: messages.role: %w", shardPath, err)
		}
		source := optionalColumn(reader, group, "source", len(contents))
		numTokens := int64Column(reader, group, "num_tokens", len(contents))

		for index := 0; index < len(contents); index++ {
			if options.Limit > 0 && summary.Records >= options.Limit {
				return nil
			}
			if options.MaxTokens > 0 && index < len(numTokens) && numTokens[index] > int64(options.MaxTokens) {
				continue
			}
			instruction, thinking, response := extractConversation(listAt(contents, index), listAt(roles, index))
			record := ReasoningRecord{
				Instruction: instruction,
				Thinking:    thinking,
				Response:    response,
				Source:      fieldAt(source, index),
			}
			if err := sink.emit(record, summary); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractConversation pulls the last user turn and the last assistant turn out
// of a role/content message list, splitting the assistant trace.
func extractConversation(contents, roles []string) (instruction, thinking, response string) {
	for index, content := range contents {
		role := ""
		if index < len(roles) {
			role = roles[index]
		}
		switch role {
		case "user", "human":
			instruction = strings.TrimSpace(content)
		case "assistant", "gpt":
			trace, answer := ParseReasoningResponse(content)
			if trace != "" {
				thinking = trace
			}
			if answer != "" {
				response = answer
			}
		}
	}
	return strings.TrimSpace(instruction), strings.TrimSpace(thinking), strings.TrimSpace(response)
}

func listAt(lists [][]string, index int) []string {
	if index < len(lists) {
		return lists[index]
	}
	return nil
}

func optionalColumn(reader *parquet.Reader, group int, name string, size int) []string {
	values, err := reader.ReadColumnStrings(group, name)
	if err != nil || len(values) != size {
		return nil
	}
	return values
}

func int64Column(reader *parquet.Reader, group int, name string, size int) []int64 {
	values, err := reader.ReadColumnInt64(group, name)
	if err != nil || len(values) != size {
		return nil
	}
	return values
}

func fieldAt(values []string, index int) string {
	if index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

// ParseReasoningResponse splits an assistant response into its reasoning trace
// and final answer. It understands the DeepSeek-R1 tag conventions used by the
// source datasets (`<think>`/`</think>`, ` thinking`/`<｜end▁of▁thinking｜>`, and gonano's
// own `<|think_start|>`/`<|think_end|>`).
func ParseReasoningResponse(response string) (thinking, answer string) {
	text := response
	for _, opener := range []string{"<|think_start|>", " thinking", "<think>"} {
		if strings.HasPrefix(text, opener) {
			text = text[len(opener):]
			break
		}
	}
	for _, closer := range []string{"<|think_end|>", "<｜end▁of▁thinking｜>", "</think>"} {
		if index := strings.Index(text, closer); index >= 0 {
			return strings.TrimSpace(text[:index]), strings.TrimSpace(text[index+len(closer):])
		}
	}
	return strings.TrimSpace(text), ""
}

// ListReasoningShards returns the parquet shard URLs for a HuggingFace dataset
// from the auto-generated parquet export API.
func ListReasoningShards(repo, config, split string) ([]string, error) {
	url := fmt.Sprintf("https://huggingface.co/api/datasets/%s/parquet/%s/%s", repo, config, split)
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("reasoning: list shards: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return nil, fmt.Errorf("reasoning: list shards: status %d", response.StatusCode)
	}
	var shards []string
	if err := json.NewDecoder(response.Body).Decode(&shards); err != nil {
		return nil, fmt.Errorf("reasoning: list shards: %w", err)
	}
	if len(shards) == 0 {
		return nil, fmt.Errorf("reasoning: no parquet shards for %s/%s/%s", repo, config, split)
	}
	return shards, nil
}

// reasoningSink streams records to chunked Markdown files and one JSONL file.
type reasoningSink struct {
	options     ReasoningOptions
	markdownDir string
	jsonlPath   string
	jsonl       *os.File
	encoder     *json.Encoder
	markdown    *os.File
	markdownSeq int
	inFileCount int
	files       []string
	closed      bool
}

func newReasoningSink(options ReasoningOptions) (*reasoningSink, error) {
	if err := os.MkdirAll(options.MarkdownDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(options.JSONLPath), 0o755); err != nil {
		return nil, err
	}
	jsonl, err := os.Create(options.JSONLPath)
	if err != nil {
		return nil, err
	}
	return &reasoningSink{
		options:     options,
		markdownDir: options.MarkdownDir,
		jsonlPath:   options.JSONLPath,
		jsonl:       jsonl,
		encoder:     json.NewEncoder(jsonl),
	}, nil
}

// emit records one flattened trace, updating the run summary. Records with
// neither a question nor a trace are dropped.
func (sink *reasoningSink) emit(record ReasoningRecord, summary *ReasoningSummary) error {
	if record.Instruction == "" && record.Thinking == "" {
		return nil
	}
	summary.Records++
	if record.Thinking != "" && record.Response != "" {
		summary.Complete++
	}
	return sink.write(record)
}

func (sink *reasoningSink) write(record ReasoningRecord) error {
	if record.Thinking != "" && record.Response != "" {
		if err := sink.encoder.Encode(record); err != nil {
			return err
		}
	}
	if sink.markdown == nil || sink.inFileCount >= sink.options.RecordsPerFile {
		if err := sink.rollMarkdown(); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(sink.markdown, formatReasoningDocument(record)); err != nil {
		return err
	}
	sink.inFileCount++
	return nil
}

func (sink *reasoningSink) rollMarkdown() error {
	if sink.markdown != nil {
		if err := sink.markdown.Close(); err != nil {
			return err
		}
	}
	name := fmt.Sprintf("%s-%05d.md", sink.options.Slug, sink.markdownSeq)
	file, err := os.Create(filepath.Join(sink.markdownDir, name))
	if err != nil {
		return err
	}
	sink.markdown = file
	sink.files = append(sink.files, filepath.Join(sink.markdownDir, name))
	sink.markdownSeq++
	sink.inFileCount = 0
	return nil
}

func (sink *reasoningSink) close() error {
	if sink.closed {
		return nil
	}
	sink.closed = true
	var firstErr error
	if sink.markdown != nil {
		if err := sink.markdown.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if sink.jsonl != nil {
		if err := sink.jsonl.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func formatReasoningDocument(record ReasoningRecord) string {
	var builder strings.Builder
	builder.WriteString("### Question\n\n")
	builder.WriteString(record.Instruction)
	builder.WriteString("\n\n")
	if record.Intent != "" {
		builder.WriteString("### What the user wants\n\n")
		builder.WriteString(record.Intent)
		builder.WriteString("\n\n")
	}
	if record.Thinking != "" {
		builder.WriteString("### Reasoning\n\n")
		builder.WriteString(record.Thinking)
		builder.WriteString("\n\n")
	}
	if record.Response != "" {
		builder.WriteString("### Answer\n\n")
		builder.WriteString(record.Response)
		builder.WriteString("\n\n")
	}
	builder.WriteString("---\n\n")
	return builder.String()
}

func logfLine(logf func(format string, args ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}
