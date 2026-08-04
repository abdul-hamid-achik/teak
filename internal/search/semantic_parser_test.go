package search

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseEnvelopeOutputRejectsMissingHits(t *testing.T) {
	if _, err := parseEnvelopeOutput([]byte(`{}`)); err == nil {
		t.Fatal("parseEnvelopeOutput({}) returned success; want schema error")
	}
}

func TestParseEnvelopeOutputAcceptsEmptyHits(t *testing.T) {
	results, err := parseEnvelopeOutput([]byte(`{"hits":[]}`))
	if err != nil {
		t.Fatalf("parseEnvelopeOutput() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("parseEnvelopeOutput() returned %d results, want 0", len(results))
	}
}

func TestParseJSONOutputRejectsMalformedOrUnrelatedObjects(t *testing.T) {
	for _, input := range []string{`{not json}`, `{}`, `{"status":"ok"}`} {
		if _, err := parseJSONOutput([]byte(input)); err == nil {
			t.Errorf("parseJSONOutput(%q) returned success; want error", input)
		}
	}
}

func TestParseJSONOutputAcceptsResultArray(t *testing.T) {
	results, err := parseJSONOutput([]byte(`[{"file":"main.go","line":3,"preview":"package main"}]`))
	if err != nil {
		t.Fatalf("parseJSONOutput() error = %v", err)
	}
	if len(results) != 1 || results[0].FilePath != "main.go" {
		t.Fatalf("parseJSONOutput() = %+v, want one main.go result", results)
	}
}

func TestParseEnvelopeOutputConvertsVecgrepLinesToZeroBased(t *testing.T) {
	results, err := parseEnvelopeOutput([]byte(`{"hits":[{"relative_path":"main.go","start_line":3,"end_line":5,"col":2,"preview":"hit"}]}`))
	if err != nil {
		t.Fatalf("parseEnvelopeOutput() error = %v", err)
	}
	if len(results) != 1 || results[0].Line != 2 || results[0].EndLine != 4 || results[0].Col != 2 {
		t.Fatalf("parseEnvelopeOutput() = %#v, want zero-based line 2/end 4 and column 2", results)
	}
}

func TestSemanticParsersBoundExternalResults(t *testing.T) {
	hits := make([]vecgrepResult, MaxSemanticSearchResults+5)
	for i := range hits {
		hits[i] = vecgrepResult{RelativePath: "file.go", Line: i, Col: 0, Preview: "hit"}
	}

	envelope, err := json.Marshal(map[string]any{"hits": hits})
	if err != nil {
		t.Fatal(err)
	}
	gotEnvelope, err := parseEnvelopeOutput(envelope)
	if err != nil {
		t.Fatalf("parseEnvelopeOutput() error = %v", err)
	}
	if len(gotEnvelope) != MaxSemanticSearchResults {
		t.Fatalf("parseEnvelopeOutput() returned %d results, want %d", len(gotEnvelope), MaxSemanticSearchResults)
	}

	array, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}
	gotArray, err := parseJSONOutput(array)
	if err != nil {
		t.Fatalf("parseJSONOutput() error = %v", err)
	}
	if len(gotArray) != MaxSemanticSearchResults {
		t.Fatalf("parseJSONOutput() returned %d results, want %d", len(gotArray), MaxSemanticSearchResults)
	}
}

func TestParsePlainOutputNormalizesLineMatches(t *testing.T) {
	results := parsePlainOutput([]byte("internal/app.go:12: package main\n\ninvalid line\nREADME.md:not-a-line: ignored\nlib.go:7\n"))
	if len(results) != 3 {
		t.Fatalf("parsePlainOutput() returned %d results, want 3: %#v", len(results), results)
	}
	want := []Result{
		{FilePath: "internal/app.go", Line: 12, Preview: "package main"},
		{FilePath: "README.md", Line: 0, Preview: "ignored"},
		{FilePath: "lib.go", Line: 7},
	}
	for index := range want {
		if results[index] != want[index] {
			t.Fatalf("result[%d] = %#v, want %#v", index, results[index], want[index])
		}
	}
}

func TestLightweightUnsupportedRecognizesLegacyOptionErrors(t *testing.T) {
	for _, message := range []string{
		"unknown flag: --lightweight",
		"flag provided but not defined: -lightweight",
		"unknown command 'status'",
		"unknown option --lightweight",
		"unrecognized option '--lightweight'",
		"invalid option --lightweight",
	} {
		if !isLightweightStatusUnsupported(errors.New(message)) {
			t.Errorf("isLightweightStatusUnsupported(%q) = false, want true", message)
		}
	}
}

func TestIsIndexedRejectsUnknownNonEmptyStatus(t *testing.T) {
	if isIndexed("vecgrep status completed successfully") {
		t.Fatal("isIndexed() accepted unknown prose as a fresh index")
	}
}
