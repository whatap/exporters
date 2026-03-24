package ncloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// SignRequest adds NCloud API authentication headers to the given HTTP request.
// NCloud API requires HMAC-SHA256 signature with these headers:
//   - x-ncp-apigw-timestamp
//   - x-ncp-iam-access-key
//   - x-ncp-apigw-signature-v2
func SignRequest(req *http.Request, accessKey, secretKey string) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	method := req.Method
	url := req.URL.RequestURI() // path + query string

	message := fmt.Sprintf("%s %s\n%s\n%s", method, url, timestamp, accessKey)
	signature := makeSignature(message, secretKey)

	req.Header.Set("x-ncp-apigw-timestamp", timestamp)
	req.Header.Set("x-ncp-iam-access-key", accessKey)
	req.Header.Set("x-ncp-apigw-signature-v2", signature)
}

func makeSignature(message, secretKey string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
