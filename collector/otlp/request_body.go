package otlp

import (
	"errors"
	"io"
	"net/http"
)

// maxOTLPRequestBodySize bounds the size of an OTLP/HTTP request body.
const maxOTLPRequestBodySize = 64 << 20

// readOTLPRequestBody reads a request body of at most maxOTLPRequestBodySize bytes.  The body is read up to the
// limit rather than sized from Content-Length, which is -1 for chunked requests and is not trusted for its size.
// An empty body is valid: it is the protobuf encoding of an empty export request.  On error, it returns the HTTP
// status code to answer with.
func readOTLPRequestBody(r *http.Request) ([]byte, int, error) {
	if r.ContentLength > maxOTLPRequestBodySize {
		return nil, http.StatusRequestEntityTooLarge, errors.New("request body exceeds size limit")
	}

	b, err := io.ReadAll(io.LimitReader(r.Body, maxOTLPRequestBodySize+1))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if len(b) > maxOTLPRequestBodySize {
		return nil, http.StatusRequestEntityTooLarge, errors.New("request body exceeds size limit")
	}
	if r.ContentLength >= 0 && int64(len(b)) != r.ContentLength {
		return nil, http.StatusBadRequest, io.ErrUnexpectedEOF
	}
	return b, http.StatusOK, nil
}
