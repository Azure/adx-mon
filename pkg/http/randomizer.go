package http

import (
	"net/http"
	"sync/atomic"
)

// closeEvery is the chance that a request will be closed.
const closeEvery = 10000 // every 10000 requests will be closed

var counter uint64

func MaybeCloseConnection(w http.ResponseWriter, r *http.Request) {
	if atomic.AddUint64(&counter, 1)%closeEvery == 0 {
		r.Close = true
		w.Header().Set("Connection", "close")
	}
}
