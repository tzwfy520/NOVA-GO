package util

import (
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// EnsureUTF8Bytes tries to decode non-UTF-8 bytes using common encodings
// and returns a UTF-8 string. If bytes are already valid UTF-8, it returns
// them as-is. If detection fails, it falls back to direct byte-to-string.
func EnsureUTF8Bytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	// Try common encodings for Chinese/legacy outputs
	encs := []encoding.Encoding{
		simplifiedchinese.GB18030,
		simplifiedchinese.GBK,
		simplifiedchinese.HZGB2312,
		traditionalchinese.Big5,
		charmap.Windows1252,
		charmap.ISO8859_1,
		charmap.Macintosh,
	}
	for _, enc := range encs {
		if s, ok := tryDecode(enc, b); ok {
			return s
		}
	}
	// Fallback: return raw bytes as string
	return string(b)
}

// EnsureUTF8 converts a possibly mojibake string to UTF-8 by decoding its bytes
// with common legacy encodings when needed.
func EnsureUTF8(s string) string {
	return EnsureUTF8Bytes([]byte(s))
}

func tryDecode(enc encoding.Encoding, b []byte) (string, bool) {
	reader := transform.NewReader(bytes.NewReader(b), enc.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", false
	}
	if utf8.Valid(decoded) {
		return string(decoded), true
	}
	return "", false
}