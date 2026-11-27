package egress

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"net/http"
	"testing"
)

const (
	phDevin   = "nightshift:secret:DEVIN_API_KEY"
	realDevin = "the-real-devin-credential-which-is-much-longer-than-its-placeholder-in-the-sandbox"
)

// getUserStatus is the body Devin CLI 3000.15.14 sent to
// SeatManagementService/GetUserStatus with the placeholder as its key,
// captured through a local proxy that answered every request itself.
const getUserStatus = "\n\xf8\x03\n\x06chisel\x12\n3000.15.14\x1a\x1fnightshift:secret:DEVIN_API_KEY\"\x02en*\x05linux:\n3000.15.14b\x06chisel\xfa\x01\xa0\x034fb15fc261605ed1eb9c196937de39ab83b966edf30ae49a7ae872d0788e59dda7dc1f31289af82e715011dafa40a0e339a82cfb4cbfb6ed380601ec43ba825436cd4f083cfbc87845fb9fafae5aae6b8889f87264c22448effb32b2e25587ed4d5d857461cd4467666261b5d8dc6b024d42d54830378165b44723861bc2af13a338b6f7b2754d1e3d6799f75a6e9939bc3271c0e1b301ed3c65b78ca16c97e35e83471ef01c4bb79f41073acd5f3ee2db16f072e557a3c9da46250b9dd05d75bc2fced58c318cf10f4de0e363390e2b"

func fixture(t *testing.T) []byte {
	t.Helper()
	return []byte(getUserStatus)
}

// field is one length-delimited protobuf field.
func field(num uint64, payload []byte) []byte {
	out := binary.AppendUvarint(nil, num<<3|2)
	out = appendVarintLen(out, len(payload))
	return append(out, payload...)
}

// lookup is the payload of the length-delimited field at path, or nil.
func lookup(t *testing.T, msg []byte, path ...uint64) []byte {
	t.Helper()
	for _, want := range path {
		var found []byte
		for i := 0; i < len(msg); {
			tag, n := binary.Uvarint(msg[i:])
			if n <= 0 {
				t.Fatalf("bad tag at %d", i)
			}
			i += n
			if tag&7 != 2 {
				size, ok := scalarSize(msg[i:], tag&7)
				if !ok {
					t.Fatalf("bad field at %d", i)
				}
				i += size
				continue
			}
			payload, used, ok := lengthDelimited(msg[i:])
			if !ok {
				t.Fatalf("bad length at %d", i)
			}
			if tag>>3 == want && found == nil {
				found = payload
			}
			i += used
		}
		if found == nil {
			return nil
		}
		msg = found
	}
	return msg
}

func swapDevin(s []byte) ([]byte, bool) {
	if !bytes.Contains(s, []byte(phDevin)) {
		return s, false
	}
	return bytes.ReplaceAll(s, []byte(phDevin), []byte(realDevin)), true
}

func protoHeader(contentType string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	return h
}

// Devin puts its key in the request metadata, a message inside the request,
// so a swap must grow both lengths around it.
func TestProtoSwapReEncodesTheLengthsAroundTheKey(t *testing.T) {
	body := fixture(t)
	if got := lookup(t, body, 1, 3); string(got) != phDevin {
		t.Fatalf("fixture metadata.api_key = %q", got)
	}
	out, changed, err := rewriteProtoBody(protoHeader("application/proto"), body, swapDevin)
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	if got := lookup(t, out, 1, 3); string(got) != realDevin {
		t.Errorf("metadata.api_key = %q, want the real key", got)
	}
	if bytes.Contains(out, []byte(phDevin)) {
		t.Error("the placeholder is still in the body")
	}
	if want := len(body) + len(realDevin) - len(phDevin); len(out) != want {
		t.Errorf("len = %d, want %d: only the key's own bytes should differ", len(out), want)
	}
	for _, f := range []uint64{1, 2, 4, 5} {
		if !bytes.Equal(lookup(t, out, 1, f), lookup(t, body, 1, f)) {
			t.Errorf("metadata field %d changed", f)
		}
	}
}

func TestProtoSwapLeavesOtherBodiesAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		contentType string
		body        []byte
		wantErr     bool
	}{
		"json":            {"application/json", []byte(`{"key":"` + phDevin + `"}`), false},
		"not protobuf":    {"application/proto", []byte("\xff\xff" + phDevin), false},
		"truncated frame": {"application/connect+proto", []byte{0, 0, 0, 0, 9, 1}, true},
	} {
		out, changed, err := rewriteProtoBody(protoHeader(tc.contentType), tc.body, swapDevin)
		if changed || !bytes.Equal(out, tc.body) || (err != nil) != tc.wantErr {
			t.Errorf("%s: changed = %v, err = %v, body = %q", name, changed, err, out)
		}
	}
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	out, err := gzipBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func envelope(t *testing.T, flags byte, payload []byte) []byte {
	t.Helper()
	out, ok := appendFrame(nil, flags, payload)
	if !ok {
		t.Fatal("frame too large")
	}
	return out
}

// A stream's frames each carry their own length, and some are compressed;
// the closing frame is JSON.
func TestProtoSwapRewritesEveryStreamFrame(t *testing.T) {
	msg := field(1, field(3, []byte(phDevin)))
	body := append(append(envelope(t, 0, msg), envelope(t, flagCompressed, gz(t, msg))...),
		envelope(t, flagEndStream, []byte(`{"error":"`+phDevin+`"}`))...)
	hdr := protoHeader("application/connect+proto")
	hdr.Set("Connect-Content-Encoding", "gzip")
	out, changed, err := rewriteProtoBody(hdr, body, swapDevin)
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	var frames [][]byte
	for rest := out; len(rest) > 0; {
		flags, payload, next, ok := splitFrame(rest)
		if !ok {
			t.Fatal("bad frame")
		}
		if flags&flagCompressed != 0 {
			var err error
			if payload, err = gunzip(payload); err != nil {
				t.Fatal(err)
			}
		}
		frames = append(frames, payload)
		rest = next
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames", len(frames))
	}
	for i, f := range frames[:2] {
		if got := lookup(t, f, 1, 3); string(got) != realDevin {
			t.Errorf("frame %d key = %q", i, got)
		}
	}
	if string(frames[2]) != `{"error":"`+realDevin+`"}` {
		t.Errorf("end frame = %q", frames[2])
	}
}

func TestProtoSwapReadsAGzippedUnaryBody(t *testing.T) {
	hdr := protoHeader("application/proto")
	hdr.Set("Content-Encoding", "gzip")
	out, changed, err := rewriteProtoBody(hdr, gz(t, fixture(t)), swapDevin)
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if _, err := plain.ReadFrom(zr); err != nil {
		t.Fatal(err)
	}
	if got := lookup(t, plain.Bytes(), 1, 3); string(got) != realDevin {
		t.Errorf("key = %q", got)
	}
}
