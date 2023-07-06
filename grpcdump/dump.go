package grpcdump

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	linePrefix    = "GRPC "
	separator     = "=== "
	statusPrefix  = "=== status"
	clientPrefix  = "=== client"
	serverPrefix  = "=== server"
	headerPrefix  = "=== header"
	trailerPrefix = "=== trailer"
)

var ErrInvalidDumpFormat = errors.New("grpcdump: invalid dump format")

// https://github.com/bradleyjkemp/grpc-tools/blob/master/grpc-dump/README.md
type Dump struct {
	Addr       string      `json:"addr"`
	FullMethod string      `json:"full_method"`
	Messages   []Message   `json:"messages"`
	Status     *Status     `json:"status"`
	Metadata   metadata.MD `json:"metadata"` // The server receives metadata.
	Header     metadata.MD `json:"header"`   // The client receives header and trailer.
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

	rows = append(rows,
		headerPrefix, writeMetadata(d.Header), "",
		trailerPrefix, writeMetadata(d.Trailer), "",
	)

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

	parseMetadata := func() (metadata.MD, error) {
		m := make(map[string]string)
		for scanner.Scan() {
			text := scanner.Text()
			if len(text) == 0 {
				break
			}

			k, v, ok := strings.Cut(text, ": ")
			if !ok {
				return nil, fmt.Errorf("%w: invalid metadata %q", ErrInvalidDumpFormat, text)
			}
			if isBinaryHeader(k) {
				b, err := decodeBinHeader(v)
				if err != nil {
					return nil, err
				}
				v = string(b)
			}

			m[k] = v
		}
		return metadata.New(m), nil
	}
	md, err := parseMetadata()
	if err != nil {
		return err
	}
	d.Metadata = md

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

	// Headers.
	header, err := parseMetadata()
	if err != nil {
		return err
	}
	d.Header = header

	trailer, err := parseMetadata()
	if err != nil {
		return err
	}
	d.Trailer = trailer

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
	return fmt.Sprintf("%s%s", linePrefix, filepath.Join(addr, fullMethod))
}

func writeMetadata(md metadata.MD) string {
	res := make([]string, 0, len(md))
	for k, vs := range md {
		b64 := func(s string) string {
			return s
		}

		if isBinaryHeader(k) {
			b64 = func(s string) string {
				// https://github.com/grpc/grpc-go/pull/1209/files
				return encodeBinHeader([]byte(s))
			}
		}

		for _, v := range vs {
			res = append(res, fmt.Sprintf("%s: %s", k, b64(v)))
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

func encodeBinHeader(v []byte) string {
	return base64.RawStdEncoding.EncodeToString(v)
}

func decodeBinHeader(v string) ([]byte, error) {
	if len(v)%4 == 0 {
		// Input was padded, or padding was not necessary.
		return base64.StdEncoding.DecodeString(v)
	}
	return base64.RawStdEncoding.DecodeString(v)
}

func isBinaryHeader(key string) bool {
	return strings.HasSuffix(key, "-bin")
}
