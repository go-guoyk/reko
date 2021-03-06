package main

import (
	"errors"
	"fmt"
	consul "github.com/hashicorp/consul/api"
	"net/http"
	"sync"
	"sync/atomic"
)

type Handler struct {
	Client *consul.Client

	RR *sync.Map
}

func NewHandler(client *consul.Client) *Handler {
	return &Handler{
		Client: client,
		RR:     &sync.Map{},
	}
}

func (h *Handler) NextRR(key string) uint64 {
	// no locking, allow inconsistency for better perf
	rr, _ := h.RR.LoadOrStore(key, new(uint64))
	// increase
	return atomic.AddUint64(rr.(*uint64), 1)
}

func (h *Handler) Rotate(key string, ups []Upstream) []Upstream {
	rr := h.NextRR(key)
	cr := rr % uint64(len(ups))
	return append(ups[cr:], ups[0:cr]...)
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	var err error
	defer func(err *error) {
		if *err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte(fmt.Sprintf("reko: %s", (*err).Error())))
		}
	}(&err)

	var q ServiceQuery
	if q, err = ExtractServiceQuery(r.URL); err != nil {
		return
	}

	var hosts []Upstream
	if hosts, err = q.Resolve(h.Client); err != nil {
		return
	}

	if len(hosts) == 0 {
		err = errors.New("no services available")
		return
	}

	// rotate hosts
	hosts = h.Rotate(q.Raw, hosts)

	// execute proxy
	NewProxy(hosts).ServeHTTP(rw, r)

}
