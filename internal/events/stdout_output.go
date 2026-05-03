package events

import (
	"encoding/json"
	"os"
)

type StdoutOutput struct {
	enc *json.Encoder
}

func NewStdoutOutput() *StdoutOutput {
	return &StdoutOutput{enc: json.NewEncoder(os.Stdout)}
}

func (s *StdoutOutput) Write(event DiagnosticEvent) error {
	return s.enc.Encode(event)
}

func (s *StdoutOutput) Flush() error { return nil }
func (s *StdoutOutput) Close() error { return nil }
