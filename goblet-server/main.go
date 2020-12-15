// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/google/goblet"
	googlehook "github.com/google/goblet/google"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	port      = flag.Int("port", 8080, "port to listen to")
	cacheRoot = flag.String("cache_root", "", "Root directory of cached repositories")

	latencyDistributionAggregation = view.Distribution(
		100,
		200,
		400,
		800,
		1000, // 1s
		2000,
		4000,
		8000,
		10000, // 10s
		20000,
		40000,
		80000,
		100000, // 100s
		200000,
		400000,
		800000,
		1000000, // 1000s
		2000000,
		4000000,
		8000000,
	)
	views = []*view.View{
		{
			Name:        "github.com/google/goblet/inbound-command-count",
			Description: "Inbound command count",
			TagKeys:     []tag.Key{goblet.CommandTypeKey, goblet.CommandCanonicalStatusKey, goblet.CommandCacheStateKey},
			Measure:     goblet.InboundCommandCount,
			Aggregation: view.Count(),
		},
		{
			Name:        "github.com/google/goblet/inbound-command-latency",
			Description: "Inbound command latency",
			TagKeys:     []tag.Key{goblet.CommandTypeKey, goblet.CommandCanonicalStatusKey, goblet.CommandCacheStateKey},
			Measure:     goblet.InboundCommandProcessingTime,
			Aggregation: latencyDistributionAggregation,
		},
		{
			Name:        "github.com/google/goblet/outbound-command-count",
			Description: "Outbound command count",
			TagKeys:     []tag.Key{goblet.CommandTypeKey, goblet.CommandCanonicalStatusKey},
			Measure:     goblet.OutboundCommandCount,
			Aggregation: view.Count(),
		},
		{
			Name:        "github.com/google/goblet/outbound-command-latency",
			Description: "Outbound command latency",
			TagKeys:     []tag.Key{goblet.CommandTypeKey, goblet.CommandCanonicalStatusKey},
			Measure:     goblet.OutboundCommandProcessingTime,
			Aggregation: latencyDistributionAggregation,
		},
		{
			Name:        "github.com/google/goblet/upstream-fetch-blocking-time",
			Description: "Duration that requests are waiting for git-fetch from the upstream",
			Measure:     goblet.UpstreamFetchWaitingTime,
			Aggregation: latencyDistributionAggregation,
		},
	}
)

func main() {
	flag.Parse()

	if err := view.Register(views...); err != nil {
		log.Fatal(err)
	}

	var er func(*http.Request, error)
	var rl func(r *http.Request, status int, requestSize, responseSize int64, latency time.Duration) = func(r *http.Request, status int, requestSize, responseSize int64, latency time.Duration) {
		dump, err := httputil.DumpRequest(r, false)
		if err != nil {
			return
		}
		log.Printf("%q %d reqsize: %d, respsize %d, latency: %v", dump, status, requestSize, responseSize, latency)
	}
	var lrol func(string, *url.URL) goblet.RunningOperation = func(action string, u *url.URL) goblet.RunningOperation {
		log.Printf("Starting %s for %s", action, u.String())
		return &logBasedOperation{action, u}
	}

	config := &goblet.ServerConfig{
		LocalDiskCacheRoot:         *cacheRoot,
		URLCanonializer:            googlehook.CanonicalizeURL,
		ErrorReporter:              er,
		RequestLogger:              rl,
		LongRunningOperationLogger: lrol,
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "ok\n")
	})
	http.Handle("/", goblet.HTTPHandler(config))
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

type LongRunningOperation struct {
	Action          string `json:"action"`
	URL             string `json:"url"`
	DurationMs      int    `json:"duration_msec,omitempty"`
	Error           string `json:"error,omitempty"`
	ProgressMessage string `json:"progress_message,omitempty"`
}

type logBasedOperation struct {
	action string
	u      *url.URL
}

func (op *logBasedOperation) Printf(format string, a ...interface{}) {
	log.Printf("Progress %s (%s): %s", op.action, op.u.String(), fmt.Sprintf(format, a...))
}

func (op *logBasedOperation) Done(err error) {
	log.Printf("Finished %s for %s: %v", op.action, op.u.String(), err)
}
