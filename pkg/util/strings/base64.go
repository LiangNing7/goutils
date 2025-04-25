package strings

import (
	"bytes"
	"encoding/base64"
	"io"
)

// DecodeBase64 接收 Base64 编码后的字符串，返回解码后 []byte.
func DecodeBase64(i string) ([]byte, error) {
	var buf bytes.Buffer
	_, err := io.Copy(
		&buf,
		base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(i)),
	)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
