package egress

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
)

// A protobuf body carries a credential inside a string field, behind length
// prefixes, so swapping a placeholder for a real value of another length
// byte-for-byte would corrupt every message around it. rewriteProtoBody walks
// the wire format instead, without a schema, and re-encodes each length it
// changes. Devin's CLI sends its key this way, as Connect RPC, in every
// request's metadata.

// protoFraming is how a body carries its protobuf messages.
type protoFraming int

const (
	protoNone protoFraming = iota
	// protoUnary is one bare message (Connect unary).
	protoUnary
	// protoEnveloped is a run of frames, each a flags byte, a big-endian
	// length, and a message (Connect streaming, gRPC, gRPC-Web).
	protoEnveloped
)

const (
	maxProtoDepth    = 32
	maxInflatedBytes = 64 << 20
	envelopeHeader   = 5
	flagCompressed   = 0x01
	flagEndStream    = 0x02 // Connect's closing frame: JSON, not protobuf
	flagTrailers     = 0x80 // gRPC-Web's trailers: text, not protobuf
)

var errProtoEncoding = errors.New("unsupported protobuf body encoding")

func protoFramingOf(contentType string) protoFraming {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(contentType))
	}
	switch mt {
	case "application/proto", "application/protobuf", "application/x-protobuf":
		return protoUnary
	case "application/connect+proto", "application/grpc", "application/grpc+proto",
		"application/grpc-web", "application/grpc-web+proto":
		return protoEnveloped
	}
	return protoNone
}

// rewriteProtoBody applies fn to every string or bytes field of the protobuf
// body that hdr describes, at any depth, and returns the body re-encoded. It
// reports whether anything changed; a body that is not protobuf, or whose
// compression it cannot read, comes back as it was.
func rewriteProtoBody(hdr http.Header, body []byte, fn func([]byte) ([]byte, bool)) (out []byte, changed bool, err error) {
	switch protoFramingOf(hdr.Get("Content-Type")) {
	case protoUnary:
		return rewriteCompressed(hdr.Get("Content-Encoding"), body, func(msg []byte) ([]byte, bool) {
			return rewriteMessage(msg, fn, 0)
		})
	case protoEnveloped:
		enc := hdr.Get("Connect-Content-Encoding")
		if enc == "" {
			enc = hdr.Get("Grpc-Encoding")
		}
		return rewriteEnvelopes(enc, body, fn)
	case protoNone:
	}
	return body, false, nil
}

// rewriteCompressed applies rewrite to body as encoding leaves it.
func rewriteCompressed(encoding string, body []byte, rewrite func([]byte) ([]byte, bool)) (out []byte, changed bool, err error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		out, changed = rewrite(body)
		return out, changed, nil
	case "gzip":
		plain, gerr := gunzip(body)
		if gerr != nil {
			return body, false, gerr
		}
		if out, changed = rewrite(plain); !changed {
			return body, false, nil
		}
		packed, zerr := gzipBytes(out)
		if zerr != nil {
			return body, false, zerr
		}
		return packed, true, nil
	}
	return body, false, fmt.Errorf("%w: %q", errProtoEncoding, encoding)
}

func rewriteEnvelopes(encoding string, body []byte, fn func([]byte) ([]byte, bool)) (out []byte, changed bool, err error) {
	out = make([]byte, 0, len(body))
	for rest := body; len(rest) > 0; {
		flags, payload, next, ok := splitFrame(rest)
		if !ok {
			return body, false, errors.New("truncated protobuf envelope")
		}
		rest = next
		frame, hit, ferr := rewriteFrame(flags, encoding, payload, fn)
		if ferr != nil {
			return body, false, ferr
		}
		if out, ok = appendFrame(out, flags, frame); !ok {
			return body, false, errors.New("protobuf frame too large")
		}
		changed = changed || hit
	}
	if !changed {
		return body, false, nil
	}
	return out, true, nil
}

// splitFrame cuts the first envelope off b.
func splitFrame(b []byte) (flags byte, payload, rest []byte, ok bool) {
	if len(b) < envelopeHeader {
		return 0, nil, nil, false
	}
	n := int64(binary.BigEndian.Uint32(b[1:envelopeHeader]))
	if n > int64(len(b)-envelopeHeader) {
		return 0, nil, nil, false
	}
	end := envelopeHeader + int(n)
	return b[0], b[envelopeHeader:end], b[end:], true
}

// appendFrame appends payload to dst as one envelope.
func appendFrame(dst []byte, flags byte, payload []byte) ([]byte, bool) {
	n := len(payload)
	if n < 0 || n > math.MaxUint32 {
		return dst, false
	}
	dst = append(dst, flags)
	dst = binary.BigEndian.AppendUint32(dst, uint32(n))
	return append(dst, payload...), true
}

func rewriteFrame(flags byte, encoding string, payload []byte, fn func([]byte) ([]byte, bool)) (out []byte, changed bool, err error) {
	rewrite := func(msg []byte) ([]byte, bool) { return rewriteMessage(msg, fn, 0) }
	if flags&(flagEndStream|flagTrailers) != 0 {
		rewrite = fn // JSON or text: a string as a whole
	}
	if flags&flagCompressed == 0 {
		out, changed = rewrite(payload)
		return out, changed, nil
	}
	return rewriteCompressed(encoding, payload, rewrite)
}

// rewriteMessage applies fn to each length-delimited field of msg: a field
// that parses as a message is walked in turn, and any other is a string or
// bytes value fn may replace. It returns msg itself when nothing changed or
// msg is not a well-formed message.
func rewriteMessage(msg []byte, fn func([]byte) ([]byte, bool), depth int) ([]byte, bool) {
	out, changed, ok := walkMessage(msg, fn, depth)
	if !ok || !changed {
		return msg, false
	}
	return out, true
}

func walkMessage(msg []byte, fn func([]byte) ([]byte, bool), depth int) (out []byte, changed, ok bool) {
	out = make([]byte, 0, len(msg))
	for i := 0; i < len(msg); {
		tag, n := binary.Uvarint(msg[i:])
		if n <= 0 || tag>>3 == 0 {
			return nil, false, false
		}
		start := i
		i += n
		if tag&7 != 2 {
			size, sok := scalarSize(msg[i:], tag&7)
			if !sok {
				return nil, false, false
			}
			i += size
			out = append(out, msg[start:i]...)
			continue
		}
		payload, used, pok := lengthDelimited(msg[i:])
		if !pok {
			return nil, false, false
		}
		i += used
		next, hit := rewriteField(payload, fn, depth+1)
		out = append(out, msg[start:start+n]...)
		out = appendVarintLen(out, len(next))
		out = append(out, next...)
		changed = changed || hit
	}
	return out, changed, true
}

// scalarSize is how many bytes a field of wire type wt takes at the start of
// b. Groups, long deprecated, and unknown types are not protobuf to it.
func scalarSize(b []byte, wt uint64) (int, bool) {
	size := 0
	switch wt {
	case 0:
		_, size = binary.Uvarint(b)
	case 1:
		size = 8
	case 5:
		size = 4
	}
	return size, size > 0 && size <= len(b)
}

// lengthDelimited is the payload a length prefix at the start of b
// announces, and how many bytes prefix and payload take together.
func lengthDelimited(b []byte) (payload []byte, used int, ok bool) {
	l, m := binary.Uvarint(b)
	if m <= 0 || l > uint64(math.MaxInt32) {
		return nil, 0, false
	}
	size := int(l)
	if size > len(b)-m {
		return nil, 0, false
	}
	return b[m : m+size], m + size, true
}

// appendVarintLen appends the length n as a protobuf varint.
func appendVarintLen(b []byte, n int) []byte {
	if n < 0 {
		return b
	}
	return binary.AppendUvarint(b, uint64(n))
}

func rewriteField(payload []byte, fn func([]byte) ([]byte, bool), depth int) ([]byte, bool) {
	if depth < maxProtoDepth {
		if sub, hit := rewriteMessage(payload, fn, depth); hit {
			return sub, true
		}
	}
	return fn(payload)
}

func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxInflatedBytes+1))
	if cerr := zr.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	if len(out) > maxInflatedBytes {
		return nil, errors.New("gzip body inflates past the limit")
	}
	return out, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip body: %w", err)
	}
	return buf.Bytes(), nil
}
