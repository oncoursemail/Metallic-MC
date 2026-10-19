package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCLIRejectsInvalidInput(t *testing.T) {
	var out, err bytes.Buffer
	code := run(nil, strings.NewReader(`{"allowSynthetic":true}`), &out, &err, time.Now())
	if code != 2 || out.Len() != 0 || err.Len() == 0 {
		t.Fatalf("unexpected invalid input result: code=%d out=%s err=%s", code, &out, &err)
	}
}

func TestCLIInsufficientEvidenceIsNotSuccessfulExit(t *testing.T) {
	var out, err bytes.Buffer
	code := run(nil, strings.NewReader(`{}`), &out, &err, time.Now())
	var result struct {
		Status          string `json:"status"`
		ProductionReady bool   `json:"productionReady"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &result); parseErr != nil {
		t.Fatal(parseErr)
	}
	if code != 1 || result.Status != "insufficient_evidence" || result.ProductionReady {
		t.Fatalf("missing evidence accepted: code=%d result=%+v", code, result)
	}
}

func TestCLIRejectsUnknownArguments(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"--deploy"}, strings.NewReader(`{}`), &out, &err, time.Now()); code != 2 {
		t.Fatalf("unexpected code %d", code)
	}
}
