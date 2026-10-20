package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/oncoursemail/metallic-mc/internal/diagnostics"
)

func TestLocalDoctorReportsWithoutEchoingSecrets(t *testing.T) {
	var out, err bytes.Buffer
	code := run(nil, strings.NewReader("private-token-xyz java.lang.OutOfMemoryError: Java heap space\n"), &out, &err)
	if code != 0 || err.Len() != 0 || !strings.Contains(out.String(), "java_heap_pressure") || strings.Contains(out.String(), "private-token-xyz") {
		t.Fatalf("code %d out %s err %s", code, &out, &err)
	}
}
func TestDoctorRejectsOversizeAndExtraArguments(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
	}{{nil, strings.Repeat("x", diagnostics.MaxLogBytes+1)}, {[]string{"unexpected"}, ""}, {[]string{"-log", "/not/a/real/metallic/log"}, ""}} {
		var out, err bytes.Buffer
		if code := run(tc.args, strings.NewReader(tc.input), &out, &err); code != 2 || out.Len() != 0 || err.Len() == 0 {
			t.Fatalf("code %d out %s err %s", code, &out, &err)
		}
	}
}
