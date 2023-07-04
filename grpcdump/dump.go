package grpcdump

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	headerPrefix = "GRPC "
	separator    = "=== "
	statusPrefix = "=== status"
	clientPrefix = "=== client"
	serverPrefix = "=== server"
)

var ErrInvalidDumpFormat = errors.New("grpcdump: invalid dump format")

// https://github.com/bradleyjkemp/grpc-tools/blob/master/grpc-dump/README.md
type Dump struct {
	Addr       string      `json:"addr"`
	FullMethod string      `json:"full_method"`
	Messages   []Message   `json:"messages"`
	Status     *Status     `json:"status"`
	Header     metadata.MD `json:"header"`
	Trailer    metadata.MD `json:"trailer"`
}

func (d *Dump) Service() string {
	return filepath.Dir(d.FullMethod)
}

func (d *Dump) Method() string {
	return filepath.Base(d.FullMethod)
}

func (d *Dump) AsText() ([]byte, error) {
	rows := []string{
		writeMethod(d.Addr, d.FullMethod),
		writeMetadata(d.Header),
		"",
	}

	msgs, err := writeMessages(d.Messages...)
	if err != nil {
		return nil, err
	}
	rows = append(rows, msgs...)

	errs, err := writeStatus(d.Status)
	if err != nil {
		return nil, err
	}
	rows = append(rows, errs...)

	res := strings.Join(rows, "\n")

	return []byte(res), nil
}

func (d *Dump) FromText(b []byte) error {
	b = bytes.TrimLeft(b, " \t\r\n")

	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		header := scanner.Text()
		if !strings.HasPrefix(header, headerPrefix) {
			return fmt.Errorf("%w: invalid line %q", ErrInvalidDumpFormat, header)
		}

		d.FullMethod = strings.TrimPrefix(header, headerPrefix)
		break
	}

	// Headers.
	m := make(map[string]string)
	for scanner.Scan() {
		text := scanner.Text()
		if len(text) == 0 {
			break
		}

		k, v, ok := strings.Cut(text, ": ")
		if !ok {
			return fmt.Errorf("%w: invalid metadata %q", ErrInvalidDumpFormat, text)
		}
		m[k] = v
	}
	d.Header = metadata.New(m)

	var sb strings.Builder
	for scanner.Scan() {
		text := scanner.Text()
		if !strings.HasPrefix(text, separator) {
			continue
		}

		sb.Reset()
		for scanner.Scan() {
			text := scanner.Text()
			if len(text) == 0 {
				break
			}

			sb.WriteString(text)
		}

		b := []byte(sb.String())
		switch text {
		case
			clientPrefix,
			serverPrefix:

			var a any
			if err := json.Unmarshal(b, &a); err != nil {
				return err
			}

			origin := strings.TrimPrefix(text, separator)

			d.Messages = append(d.Messages, Message{
				Origin:  origin,
				Message: a,
			})
		case statusPrefix:
			var e Status
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}

			d.Status = &e
		}
	}

	return nil
}

func writeStatus(e *Status) ([]string, error) {
	if e == nil {
		return nil, nil
	}

	b, err := json.MarshalIndent(e, "", " ")
	if err != nil {
		return nil, err
	}

	return []string{fmt.Sprintf("%s\n%s\n", statusPrefix, b)}, nil
}

func writeMethod(addr, fullMethod string) string {
	return fmt.Sprintf("%s%s", headerPrefix, filepath.Join(addr, fullMethod))
}

func writeMetadata(md metadata.MD) string {
	res := make([]string, 0, len(md))
	for k, vs := range md {
		for _, v := range vs {
			res = append(res, fmt.Sprintf("%s: %s", k, v))
		}
	}

	return strings.Join(res, "\n")
}

func writeMessages(msgs ...Message) ([]string, error) {
	res := make([]string, len(msgs))
	for i, msg := range msgs {
		b, err := json.MarshalIndent(msg.Message, "", " ")
		if err != nil {
			return nil, err
		}

		if msg.Origin == OriginServer {
			res[i] = fmt.Sprintf("%s\n%s\n\n", serverPrefix, b)
		} else {
			res[i] = fmt.Sprintf("%s\n%s\n\n", clientPrefix, b)
		}
	}

	return res, nil
}
