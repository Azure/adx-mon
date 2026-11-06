package otlp

import (
	"errors"
	"io"
	"net/http"
)

const maxOTLPRequestBodySize = 64 << 20

func readOTLPRequestBody(r *http.Request) ([]byte, int, error) {
	if r.ContentLength < 0 {
		return nil, http.StatusLengthRequired, errors.New("request Content-Length is required")
	}
	if r.ContentLength > maxOTLPRequestBodySize {
		return nil, http.StatusRequestEntityTooLarge, errors.New("request body exceeds size limit")
	}

	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxOTLPRequestBodySize))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, http.StatusRequestEntityTooLarge, err
		}
		return nil, http.StatusInternalServerError, err
	}
	if int64(len(b)) != r.ContentLength {
		return nil, http.StatusBadRequest, io.ErrUnexpectedEOF
	}
	return b, http.StatusOK, nil
}
