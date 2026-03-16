package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"time"

	"tlsctl/internal/runner"
)

func main() {
	var text string
	flag.StringVar(&text, "text", "", "semi-structured text input")
	flag.Parse()

	var req runner.Request
	var err error
	if text != "" {
		req, err = runner.RequestFromText(text)
	} else {
		req, err = readJSON(os.Stdin)
	}
	if err != nil {
		writeJSON(os.Stdout, runner.Response{Error: &runner.RespError{Code: runner.ErrValidation, Message: err.Error()}})
		return
	}
	r := runner.Runner{Now: func() time.Time { return time.Now() }}
	resp := r.Execute(context.Background(), req)
	writeJSON(os.Stdout, resp)
}

func readJSON(r io.Reader) (runner.Request, error) {
	var req runner.Request
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		return runner.Request{}, err
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	return req, nil
}

func writeJSON(w io.Writer, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	_, _ = w.Write(append(b, '\n'))
}
