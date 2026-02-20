package bastion

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBastion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bastion Controller Suite")
}

type inMemoryLogSink struct {
	Buf *bytes.Buffer
}

func newInMemoryLogSink() *inMemoryLogSink {
	return &inMemoryLogSink{
		Buf: bytes.NewBuffer([]byte{}),
	}
}

func (s *inMemoryLogSink) Init(logr.RuntimeInfo) {
	// no-op
}

func (s *inMemoryLogSink) Enabled(int) bool {
	return true
}

func (s *inMemoryLogSink) Info(level int, msg string, keysAndValues ...any) {
	_, err := fmt.Fprint(s.Buf, "INFO:", level, msg)
	if err != nil {
		panic(err)
	}
	_, err = fmt.Fprintln(s.Buf, keysAndValues...)
	if err != nil {
		panic(err)
	}
}

func (s *inMemoryLogSink) Error(err error, msg string, keysAndValues ...any) {
	_, printErr := fmt.Fprint(s.Buf, "ERROR:", err, msg)
	if printErr != nil {
		panic(printErr)
	}
	_, printErr = fmt.Fprintln(s.Buf, keysAndValues...)
	if printErr != nil {
		panic(printErr)
	}
}

func (s *inMemoryLogSink) WithValues(keysAndValues ...any) logr.LogSink {
	return s
}

func (s *inMemoryLogSink) WithName(string) logr.LogSink {
	return s
}
