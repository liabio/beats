// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package log

import (
	"sync"
	"time"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/common"
	"github.com/elastic/beats/v7/libbeat/logp"
	"github.com/elastic/beats/v7/libbeat/monitoring"
	"github.com/elastic/beats/v7/libbeat/monitoring/report"
)

// List of metrics that are gauges. This is used to identify metrics that should
// not be reported as deltas. Instead we log the raw value if there was any
// observable change during the interval.
//
// TODO: Replace this with a proper solution that uses the metric type from
// where it is defined. See: https://github.com/elastic/beats/issues/5433
var gauges = map[string]bool{
	"libbeat.pipeline.events.active": true,
	"libbeat.pipeline.clients":       true,
	"libbeat.config.module.running":  true,
	"registrar.states.current":       true,
	"filebeat.harvester.running":     true,
	"filebeat.harvester.open_files":  true,
	"beat.memstats.memory_total":     true,
	"beat.memstats.memory_alloc":     true,
	"beat.memstats.gc_next":          true,
	"beat.info.uptime.ms":            true,
	"beat.cpu.user.ticks":            true,
	"beat.cpu.user.time":             true,
	"beat.cpu.system.ticks":          true,
	"beat.cpu.system.time":           true,
	"beat.cpu.total.value":           true,
	"beat.cpu.total.ticks":           true,
	"beat.cpu.total.time":            true,
	"beat.handles.open":              true,
	"beat.handles.limit.hard":        true,
	"beat.handles.limit.soft":        true,
	"beat.runtime.goroutines":        true,
	"system.load.1":                  true,
	"system.load.5":                  true,
	"system.load.15":                 true,
	"system.load.norm.1":             true,
	"system.load.norm.5":             true,
	"system.load.norm.15":            true,
}

// TODO: Change this when gauges are refactored, too.
var strConsts = map[string]bool{
	"beat.info.ephemeral_id": true,
}

var (
	// StartTime is the time that the process was started.
	StartTime = time.Now()
)

type reporter struct {
	wg       sync.WaitGroup
	done     chan struct{}
	period   time.Duration
	registry *monitoring.Registry

	// output
	logger *logp.Logger
}

// MakeReporter returns a new Reporter that periodically reports metrics via
// logp. If cfg is nil defaults will be used.
func MakeReporter(beat beat.Info, cfg *common.Config) (report.Reporter, error) {
	config := defaultConfig
	if cfg != nil {
		if err := cfg.Unpack(&config); err != nil {
			return nil, err
		}
	}

	r := &reporter{
		done:     make(chan struct{}),
		period:   config.Period,
		logger:   logp.NewLogger("monitoring"),
		registry: monitoring.Default,
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.snapshotLoop()
	}()
	return r, nil
}

func (r *reporter) Stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *reporter) snapshotLoop() {
	//Starting metrics logging every 30s
	r.logger.Infof("Starting metrics logging every %v", r.period)
	// Stopping metrics logging.
	defer r.logger.Infof("Stopping metrics logging.")
	defer func() {
		r.logTotals(makeDeltaSnapshot(monitoring.MakeFlatSnapshot(), makeSnapshot(r.registry)))
	}()

	ticker := time.NewTicker(r.period)
	defer ticker.Stop()

	var last monitoring.FlatSnapshot
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
		}

		cur := makeSnapshot(r.registry)
		delta := makeDeltaSnapshot(last, cur)
		last = cur

		r.logSnapshot(delta)
	}
}

func (r *reporter) logSnapshot(s monitoring.FlatSnapshot) {
	if snapshotLen(s) > 0 {
		//示例： Non-zero metrics in the last 30s        {"monitoring": {"metrics": {"beat":{"cpu":{"system":{"ticks":70,"time":{"ms":8}},"total":{"ticks":360,"time":{"ms":14},"value":360},"user":{"ticks":290,"time":{"ms":6}}},"handles":{"limit":{"hard":102536,"soft":102536},"open":15},"info":{"ephemeral_id":"87ec1897-967e-4e86-99e0-1ccaca647168","uptime":{"ms":60072}},"memstats":{"gc_next":22120896,"memory_alloc":16758552,"memory_total":74224816,"rss":536576},"runtime":{"goroutines":79}},"filebeat":{"events":{"added":24,"done":24},"harvester":{"open_files":1,"running":1}},"libbeat":{"config":{"module":{"running":1}},"output":{"events":{"acked":24,"batches":2,"total":24}},"outputs":{"kafka":{"bytes_read":397,"bytes_write":5508}},"pipeline":{"clients":6,"events":{"active":0,"published":24,"total":24},"queue":{"acked":24}}},"processor":{"javascript":{"enable":{"histogram":{"process_time":{"count":24,"mean":-1050.2773061282733,"median":-176,"p75":-1821,"p95":-3028.799999999992,"p99":-30971.51999999932,"stddev":-1647.2586037812289}}}}},"registrar":{"states":{"current":19,"update":24},"writes":{"success":2,"total":2}},"system":{"load":{"1":0.02,"15":0.06,"5":0.06,"norm":{"1":0.0025,"15":0.0075,"5":0.0075}}}}}}
		r.logger.Infow("Non-zero metrics in the last "+r.period.String(), toKeyValuePairs(s)...)
		return
	}

	r.logger.Infof("No non-zero metrics in the last %v", r.period)
}

func (r *reporter) logTotals(s monitoring.FlatSnapshot) {
	//示例：Total non-zero metrics  {"monitoring": {"metrics": {"beat":{"cpu":{"system":{"ticks":70,"time":{"ms":72}},"total":{"ticks":360,"time":{"ms":368},"value":360},"user":{"ticks":290,"time":{"ms":296}}},"handles":{"limit":{"hard":102536,"soft":102536},"open":13},"info":{"ephemeral_id":"87ec1897-967e-4e86-99e0-1ccaca647168","uptime":{"ms":63244}},"memstats":{"gc_next":22120896,"memory_alloc":17173144,"memory_total":74639408,"rss":55848960},"runtime":{"goroutines":28}},"filebeat":{"events":{"active":1,"added":152,"done":151},"harvester":{"closed":1,"open_files":0,"running":0,"started":1}},"libbeat":{"config":{"module":{"running":1,"starts":1},"reloads":1,"scans":1},"output":{"events":{"acked":131,"batches":6,"total":131},"type":"kafka"},"outputs":{"kafka":{"bytes_read":1552,"bytes_write":19344}},"pipeline":{"clients":0,"events":{"active":1,"filtered":20,"published":132,"retry":98,"total":152},"queue":{"acked":131}}},"processor":{"javascript":{"enable":{"histogram":{"process_time":{"count":132,"max":200639,"mean":12006.939393939394,"median":10180,"min":3674,"p75":11232.5,"p95":29555.649999999994,"p99":158053.1599999984,"p999":200639,"stddev":18995.86028855729}}}}},"registrar":{"states":{"current":19,"update":151},"writes":{"success":26,"total":26}},"system":{"cpu":{"cores":8},"load":{"1":0.02,"15":0.06,"5":0.06,"norm":{"1":0.0025,"15":0.0075,"5":0.0075}}}}}}
	r.logger.Infow("Total non-zero metrics", toKeyValuePairs(s)...)
	//Uptime: 1m3.247213679s
	r.logger.Infof("Uptime: %v", time.Since(StartTime))
}

func makeSnapshot(R *monitoring.Registry) monitoring.FlatSnapshot {
	mode := monitoring.Full
	return monitoring.CollectFlatSnapshot(R, mode, true)
}

func makeDeltaSnapshot(prev, cur monitoring.FlatSnapshot) monitoring.FlatSnapshot {
	delta := monitoring.MakeFlatSnapshot()

	for k, b := range cur.Bools {
		if p, ok := prev.Bools[k]; !ok || p != b {
			delta.Bools[k] = b
		}
	}

	for k, s := range cur.Strings {
		if _, found := strConsts[k]; found {
			delta.Strings[k] = s
		} else if p, ok := prev.Strings[k]; !ok || p != s {
			delta.Strings[k] = s
		}
	}

	for k, i := range cur.Ints {
		if _, found := gauges[k]; found {
			delta.Ints[k] = i
		} else {
			if p := prev.Ints[k]; p != i {
				delta.Ints[k] = i - p
			}
		}
	}

	for k, f := range cur.Floats {
		if _, found := gauges[k]; found {
			delta.Floats[k] = f
		} else if p := prev.Floats[k]; p != f {
			delta.Floats[k] = f - p
		}
	}

	return delta
}

func snapshotLen(s monitoring.FlatSnapshot) int {
	return len(s.Bools) + len(s.Floats) + len(s.Ints) + len(s.Strings)
}

func toKeyValuePairs(s monitoring.FlatSnapshot) []interface{} {
	data := make(common.MapStr, snapshotLen(s))
	for k, v := range s.Bools {
		data.Put(k, v)
	}
	for k, v := range s.Floats {
		data.Put(k, v)
	}
	for k, v := range s.Ints {
		data.Put(k, v)
	}
	for k, v := range s.Strings {
		data.Put(k, v)
	}

	return []interface{}{logp.Namespace("monitoring"), logp.Reflect("metrics", data)}
}
