package cli

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
)

type TerminalIO interface {
	ReadLine(ctx context.Context) (string, error)
	Write(text string) error
	WriteLine(text string) error
}

type stdio struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

func newStdio(reader io.Reader, writer io.Writer) TerminalIO {
	return &stdio{
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

func (s *stdio) ReadLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}

	resultCh := make(chan result, 1)
	go func() {
		line, err := s.reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if err == io.EOF && line != "" {
			err = nil
		}
		resultCh <- result{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		return result.line, result.err
	}
}

func (s *stdio) Write(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := io.WriteString(s.writer, text)
	return err
}

func (s *stdio) WriteLine(text string) error {
	return s.Write(text + "\n")
}
