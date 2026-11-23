package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

var namedParamRe = regexp.MustCompile(`:([a-zA-Z_]\w*)`)

type WorkflowExecutor struct {
	dataDB *sql.DB
}

func NewWorkflowExecutor(dataDB *sql.DB) *WorkflowExecutor {
	return &WorkflowExecutor{dataDB: dataDB}
}

type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Stage   string `json:"stage,omitempty"`
}

type StageResult struct {
	Stage       string `json:"stage"`
	Output      any    `json:"output"`
	Error       string `json:"error,omitempty"`
	ExecutedSQL string `json:"executedSql,omitempty"`
}

type WorkflowResult struct {
	Stages []StageResult `json:"stages"`
	Output any           `json:"output,omitempty"`
	Error  string        `json:"error,omitempty"`
	Logs   []LogEntry    `json:"logs,omitempty"`
}

func (e *WorkflowExecutor) Execute(ctx context.Context, wf *database.WorkflowRow, input map[string]any) *WorkflowResult {
	result := &WorkflowResult{}
	var allLogs []LogEntry
	tagLogs := func(logs []LogEntry, stage string) {
		for i := range logs {
			logs[i].Stage = stage
		}
		allLogs = append(allLogs, logs...)
	}

	// Stage 1: input transformation
	jsRes, err := runJS(wf.InputScript, map[string]any{"input": input})
	tagLogs(jsRes.logs, "input_script")
	if err != nil {
		result.Stages = append(result.Stages, StageResult{Stage: "input_script", Error: err.Error()})
		result.Error = fmt.Sprintf("input_script: %v", err)
		result.Logs = allLogs
		return result
	}
	result.Stages = append(result.Stages, StageResult{Stage: "input_script", Output: jsRes.output})

	inputMap, ok := jsRes.output.(map[string]any)
	if !ok {
		inputMap = map[string]any{}
	}

	// Stage 2: SQL queries grouped by stage (sequential stages, parallel within)
	type queryResult struct {
		idx         int
		label       string
		rows        []map[string]any
		errMsg      string
		executedSQL string
	}

	stageGroups := make(map[int][]int)
	for i, q := range wf.Queries {
		stageGroups[q.Stage] = append(stageGroups[q.Stage], i)
	}
	stageNums := make([]int, 0, len(stageGroups))
	for s := range stageGroups {
		stageNums = append(stageNums, s)
	}
	sort.Ints(stageNums)

	queryOutputs := make(map[string]any)
	var queryErr string

	for _, stageNum := range stageNums {
		indices := stageGroups[stageNum]
		qResults := make([]queryResult, len(indices))

		var wg sync.WaitGroup
		for gi, qi := range indices {
			wg.Add(1)
			go func(groupIdx, queryIdx int) {
				defer wg.Done()
				query := wf.Queries[queryIdx]
				qr := queryResult{idx: queryIdx, label: query.Label}

				scope := make(map[string]any, len(inputMap)+len(queryOutputs))
				for k, v := range queryOutputs {
					scope[k] = v
				}
				for k, v := range inputMap {
					scope[k] = v
				}
				resolved, args := resolveNamedParams(query.SQLText, scope)
				qr.executedSQL = interpolateSQL(resolved, args)

				rows, err := e.queryRows(ctx, resolved, args)
				if err != nil {
					qr.errMsg = err.Error()
				} else {
					qr.rows = rows
				}
				qResults[groupIdx] = qr
			}(gi, qi)
		}
		wg.Wait()

		for _, qr := range qResults {
			sr := StageResult{Stage: fmt.Sprintf("query:%d:%s", stageNum, qr.label), ExecutedSQL: qr.executedSQL}
			if qr.errMsg != "" {
				sr.Error = qr.errMsg
				if queryErr == "" {
					queryErr = fmt.Sprintf("query %q: %s", qr.label, qr.errMsg)
				}
			} else {
				sr.Output = qr.rows
				queryOutputs[qr.label] = qr.rows
			}
			result.Stages = append(result.Stages, sr)
		}

		if queryErr != "" {
			break
		}
	}

	if queryErr != "" {
		result.Error = queryErr
		result.Logs = allLogs
		return result
	}

	// Stage 3: output transformation
	outputInput := map[string]any{
		"input":   jsRes.output,
		"results": queryOutputs,
	}
	outRes, err := runJS(wf.OutputScript, outputInput)
	tagLogs(outRes.logs, "output_script")
	if err != nil {
		result.Stages = append(result.Stages, StageResult{Stage: "output_script", Error: err.Error()})
		result.Error = fmt.Sprintf("output_script: %v", err)
		result.Logs = allLogs
		return result
	}
	result.Stages = append(result.Stages, StageResult{Stage: "output_script", Output: outRes.output})
	result.Output = outRes.output
	result.Logs = allLogs

	return result
}

// jsTimeout bounds one workflow script run so a runaway loop cannot pin a
// CPU indefinitely. It matches the workflow proxy's request timeout.
var jsTimeout = 30 * time.Second

type jsResult struct {
	output any
	logs   []LogEntry
}

func runJS(script string, globals map[string]any) (*jsResult, error) {
	vm := goja.New()
	var logs []LogEntry
	for k, v := range globals {
		if err := vm.Set(k, v); err != nil {
			return &jsResult{logs: logs}, fmt.Errorf("set global %q: %w", k, err)
		}
	}
	console := vm.NewObject()
	makeLogger := func(level string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			parts := make([]string, len(call.Arguments))
			for i, arg := range call.Arguments {
				parts[i] = arg.String()
			}
			logs = append(logs, LogEntry{Level: level, Message: strings.Join(parts, " ")})
			return goja.Undefined()
		}
	}
	err := console.Set("log", makeLogger("log"))
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set log: %w", err)
	}
	err = console.Set("info", makeLogger("info"))
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set info: %w", err)
	}
	err = console.Set("warn", makeLogger("warn"))
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set warn: %w", err)
	}
	err = console.Set("error", makeLogger("error"))
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set error: %w", err)
	}
	err = console.Set("debug", makeLogger("debug"))
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set debug: %w", err)
	}
	err = vm.Set("console", console)
	if err != nil {
		return &jsResult{logs: logs}, fmt.Errorf("set console: %w", err)
	}

	timer := time.AfterFunc(jsTimeout, func() { vm.Interrupt("script exceeded " + jsTimeout.String()) })
	defer timer.Stop()
	val, err := vm.RunString(script)
	if err != nil {
		return &jsResult{logs: logs}, err
	}
	return &jsResult{output: val.Export(), logs: logs}, nil
}

func resolveNamedParams(sqlText string, scope map[string]any) (string, []any) {
	var args []any
	var buf strings.Builder
	buf.Grow(len(sqlText))
	i := 0

	for i < len(sqlText) {
		c := sqlText[i]

		// Line comment: copy unchanged through end of line
		if c == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-' {
			j := strings.IndexByte(sqlText[i:], '\n')
			if j < 0 {
				buf.WriteString(sqlText[i:])
				break
			}
			buf.WriteString(sqlText[i : i+j+1])
			i += j + 1
			continue
		}

		// Block comment: copy unchanged through */
		if c == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*' {
			end := strings.Index(sqlText[i+2:], "*/")
			if end < 0 {
				buf.WriteString(sqlText[i:])
				break
			}
			end += i + 4
			buf.WriteString(sqlText[i:end])
			i = end
			continue
		}

		// Single-quoted string literal: copy unchanged
		if c == '\'' {
			j := i + 1
			for j < len(sqlText) {
				if sqlText[j] == '\'' {
					if j+1 < len(sqlText) && sqlText[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				if sqlText[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(sqlText) {
				j++
			}
			buf.WriteString(sqlText[i:j])
			i = j
			continue
		}

		// Named parameter
		if c == ':' {
			loc := namedParamRe.FindStringIndex(sqlText[i:])
			if loc != nil && loc[0] == 0 {
				key := sqlText[i+1 : i+loc[1]]
				v := scope[key]
				if slice, ok := toSlice(v); ok {
					if len(slice) == 0 {
						buf.WriteByte('?')
						args = append(args, nil)
					} else {
						for si, elem := range slice {
							if si > 0 {
								buf.WriteString(", ")
							}
							buf.WriteByte('?')
							args = append(args, elem)
						}
					}
				} else {
					buf.WriteByte('?')
					args = append(args, v)
				}
				i += loc[1]
				continue
			}
		}

		buf.WriteByte(c)
		i++
	}

	return buf.String(), args
}

func interpolateSQL(query string, args []any) string {
	var buf strings.Builder
	buf.Grow(len(query))
	argIdx := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' && argIdx < len(args) {
			buf.WriteString(formatSQLValue(args[argIdx]))
			argIdx++
		} else {
			buf.WriteByte(query[i])
		}
	}
	return buf.String()
}

func formatSQLValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case string:
		escaped := strings.ReplaceAll(val, "'", "''")
		return "'" + escaped + "'"
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		escaped := strings.ReplaceAll(fmt.Sprintf("%v", val), "'", "''")
		return "'" + escaped + "'"
	}
}

func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, true
	default:
		return nil, false
	}
}

func (e *WorkflowExecutor) queryRows(ctx context.Context, query string, args []any) ([]map[string]any, error) {
	rows, err := e.dataDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := vals[i]
			switch vt := v.(type) {
			case []byte:
				var parsed any
				if err := json.Unmarshal(vt, &parsed); err == nil {
					row[col] = parsed
				} else {
					row[col] = string(vt)
				}
			default:
				row[col] = v
			}
		}
		results = append(results, row)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return results, rows.Err()
}

type TestCaseResult struct {
	Name   string     `json:"name"`
	Passed bool       `json:"passed"`
	Error  string     `json:"error,omitempty"`
	Output any        `json:"output,omitempty"`
	Logs   []LogEntry `json:"logs,omitempty"`
}

const jsonSchemaValidatorJS = `
(function() {
  function validate(schema, data, path) {
    var errors = [];
    if (schema.type) {
      var t = schema.type;
      var actual = data === null ? 'null' : Array.isArray(data) ? 'array' : typeof data;
      if (t === 'integer') { if (typeof data !== 'number' || data % 1 !== 0) errors.push(path + ': expected integer, got ' + actual); }
      else if (t !== actual) errors.push(path + ': expected ' + t + ', got ' + actual);
    }
    if (schema.enum) {
      var found = false;
      for (var i = 0; i < schema.enum.length; i++) { if (JSON.stringify(data) === JSON.stringify(schema.enum[i])) { found = true; break; } }
      if (!found) errors.push(path + ': value not in enum');
    }
    if (typeof data === 'number') {
      if (schema.minimum !== undefined && data < schema.minimum) errors.push(path + ': minimum ' + schema.minimum);
      if (schema.maximum !== undefined && data > schema.maximum) errors.push(path + ': maximum ' + schema.maximum);
    }
    if (typeof data === 'string') {
      if (schema.minLength !== undefined && data.length < schema.minLength) errors.push(path + ': minLength ' + schema.minLength);
      if (schema.maxLength !== undefined && data.length > schema.maxLength) errors.push(path + ': maxLength ' + schema.maxLength);
      if (schema.pattern) { var re = new RegExp(schema.pattern); if (!re.test(data)) errors.push(path + ': pattern mismatch'); }
    }
    if (schema.properties && typeof data === 'object' && data !== null && !Array.isArray(data)) {
      var keys = Object.keys(schema.properties);
      for (var i = 0; i < keys.length; i++) {
        if (data[keys[i]] !== undefined) {
          errors = errors.concat(validate(schema.properties[keys[i]], data[keys[i]], path + '.' + keys[i]));
        }
      }
      if (schema.additionalProperties === false) {
        var allowed = Object.keys(schema.properties);
        var dataKeys = Object.keys(data);
        for (var i = 0; i < dataKeys.length; i++) {
          if (allowed.indexOf(dataKeys[i]) === -1) errors.push(path + '.' + dataKeys[i] + ': additional property not allowed');
        }
      }
    }
    if (schema.required && typeof data === 'object' && data !== null) {
      for (var i = 0; i < schema.required.length; i++) {
        if (data[schema.required[i]] === undefined) errors.push(path + ': missing required property ' + schema.required[i]);
      }
    }
    if (schema.items && Array.isArray(data)) {
      for (var i = 0; i < data.length; i++) {
        errors = errors.concat(validate(schema.items, data[i], path + '[' + i + ']'));
      }
    }
    return errors;
  }
  var errs = validate(schema, data, '$');
  if (errs.length > 0) throw new Error('Schema validation failed:\n' + errs.join('\n'));
})()
`

func validateJSONSchema(schemaText string, data any) error {
	var schema any
	if err := json.Unmarshal([]byte(schemaText), &schema); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}
	vm := goja.New()
	err := vm.Set("schema", schema)
	if err != nil {
		return fmt.Errorf("set schema: %w", err)
	}
	err = vm.Set("data", data)
	if err != nil {
		return fmt.Errorf("set data: %w", err)
	}
	_, err = vm.RunString(jsonSchemaValidatorJS)
	if err != nil {
		return fmt.Errorf("run script: %w", err)
	}
	return err
}

func (e *WorkflowExecutor) RunTests(ctx context.Context, wf *database.WorkflowRow, schemas map[int64]string) []TestCaseResult {
	results := make([]TestCaseResult, len(wf.Tests))
	for i, tc := range wf.Tests {
		results[i].Name = tc.Name

		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Input), &input); err != nil {
			results[i].Error = "invalid test input JSON: " + err.Error()
			continue
		}

		execResult := e.Execute(ctx, wf, input)
		results[i].Output = execResult.Output
		results[i].Logs = execResult.Logs
		if execResult.Error != "" {
			results[i].Error = "execution error: " + execResult.Error
			continue
		}

		pipelineError := execResult.Error
		output := execResult.Output

		// Schema validation runs before assertion scripts.
		if tc.SchemaID > 0 {
			schemaText, ok := schemas[tc.SchemaID]
			if !ok {
				results[i].Error = "schema not found: " + strconv.FormatInt(tc.SchemaID, 10)
				continue
			}
			if pipelineError != "" {
				results[i].Error = "pipeline error: " + pipelineError
				continue
			}
			if err := validateJSONSchema(schemaText, output); err != nil {
				results[i].Error = "schema validation: " + err.Error()
				continue
			}
		}

		assertScript := strings.TrimSpace(tc.AssertScript)
		if assertScript == "" {
			if pipelineError != "" {
				results[i].Error = "pipeline error: " + pipelineError
			} else {
				results[i].Passed = true
			}
			continue
		}

		globals := map[string]any{"output": nil, "result": nil, "error": nil}
		if pipelineError != "" {
			globals["error"] = pipelineError
		} else {
			globals["output"] = output
			globals["result"] = output
		}

		jsRes, err := runJS(assertScript, globals)
		if jsRes != nil {
			for _, l := range jsRes.logs {
				results[i].Logs = append(results[i].Logs, LogEntry{Level: l.Level, Message: l.Message, Stage: "assertion"})
			}
		}
		if err != nil {
			results[i].Error = err.Error()
		} else {
			results[i].Passed = true
		}
	}
	return results
}
