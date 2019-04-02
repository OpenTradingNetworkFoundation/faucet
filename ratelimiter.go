package main

import (
	"net/http"
	"sync"

	"github.com/didip/tollbooth/libstring"
	"github.com/juju/ratelimit"
)

type BucketFactory func() *ratelimit.Bucket

type RateLimiter struct {
	bucketFactory BucketFactory
	buckets       map[string]*ratelimit.Bucket
	mutex         sync.Mutex
}

func NewRateLimiter(f BucketFactory) *RateLimiter {
	return &RateLimiter{
		bucketFactory: f,
		buckets:       make(map[string]*ratelimit.Bucket),
	}
}

func (r *RateLimiter) Get(key string) *ratelimit.Bucket {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	b, ok := r.buckets[key]
	if !ok {
		b = r.bucketFactory()
		r.buckets[key] = b
	}

	return b
}

var ipLookupFields = []string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"}

func RemoteIP(r *http.Request) string {
	return libstring.RemoteIP(ipLookupFields, 0, r)
}
