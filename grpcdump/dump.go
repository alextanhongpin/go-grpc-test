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

var ErrInvalidDumpFormat = errors.New("grpcdump: invalid dump format")

// https://github.com/bradleyjkemp/grpc-tools/blob/master/grpc-dump/README.md
type Dump struct {
	FullMethod string      `json:"full_method"`
	Messages   []Message   `json:"messages"`
	Error      *Error      `json:"error"`
	Metadata   metadata.MD `json:"metadata"`
}

func (d *Dump) Service() string {
	return filepath.Dir(d.FullMethod)
}

func (d *Dump) Method() string {
	return filepath.Base(d.FullMethod)
}

func (d *Dump) AsText() ([]byte, error) {
	rows := []string{
		writeMethod(d.FullMethod),
		writeMetadata(d.Metadata),
		"",
	}

	msgs, err := writeMessages(d.Messages...)
	if err != nil {
		return nil, err
	}
	rows = append(rows, msgs...)

	errs, err := writeError(d.Error)
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
		if !strings.HasPrefix(header, "GRPC ") {
			return fmt.Errorf("%w: invalid line %q", ErrInvalidDumpFormat, header)
		}

		d.FullMethod = strings.TrimPrefix(header, "GRPC ")
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
	d.Metadata = metadata.New(m)

	var sb strings.Builder
	for scanner.Scan() {
		text := scanner.Text()
		if !strings.HasPrefix(text, "#") {
			continue
		}

		prefix := strings.TrimPrefix(text, "# ")

		sb.Reset()
		for scanner.Scan() {
			text := scanner.Text()
			if len(text) == 0 {
				break
			}

			sb.WriteString(text)
		}

		b := []byte(sb.String())
		switch prefix {
		case
			OriginClient,
			OriginServer:
			var a any
			if err := json.Unmarshal(b, &a); err != nil {
				return err
			}
			d.Messages = append(d.Messages, Message{
				Origin:  prefix,
				Message: a,
			})
		case "error":
			var e Error
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}
			d.Error = &e
		}
	}

	return nil
}

func writeError(e *Error) ([]string, error) {
	if e == nil {
		return nil, nil
	}

	b, err := json.MarshalIndent(e, "", " ")
	if err != nil {
		return nil, err
	}

	return []string{fmt.Sprintf("# error\n%s\n", b)}, nil
}

func writeMethod(fullMethod string) string {
	return fmt.Sprintf("GRPC %s", fullMethod)
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

		res[i] = fmt.Sprintf("# %s\n%s\n\n", msg.Origin, b)
	}

	return res, nil
}
