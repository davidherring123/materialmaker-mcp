package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRejectsOldCLICommands(t *testing.T) {
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"list"}, strings.NewReader(""), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "unexpected command") {
		t.Fatalf("expected CLI rejection, got %v", err)
	}
}
